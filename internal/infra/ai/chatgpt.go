package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"scanner-monitor/internal/domain"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type inputDTO struct {
	ClassificationName     *string `json:"classification_name" jsonschema:"enum=identidade,enum=certidao_nascimento,enum=prontuario,enum=carteira_vacinacao,enum=historico_escolar,enum=atestado,enum=formulario,enum=documento_aluno,enum=outro,enum=documentos_mistos" jsonschema_description:"Tipo principal do documento; formulario para modelos inteiramente em branco; documentos_mistos para conjuntos sem tipo principal"`
	IsBlankForm            *bool   `json:"is_blank_form" jsonschema_description:"True somente para modelo reutilizável inteiramente sem preenchimento pessoal em todas as páginas; assinaturas, marcações, registros manuscritos e dados parciais tornam false"`
	HolderRole             *string `json:"holder_role" jsonschema:"enum=aluno,enum=titular,enum=responsavel,enum=indeterminado" jsonschema_description:"Papel do titular selecionado: aluno explícito, titular único do documento, responsável explícito ou indeterminado quando houver dúvida"`
	IdentificationStatus   *string `json:"identification_status" jsonschema:"enum=identificado,enum=ausente,enum=ilegivel,enum=ambiguo" jsonschema_description:"Identificado exige nome completo legível do aluno ou titular correto; ausente se não houver identificação do aluno; ilegivel para escrita que não permite transcrição segura; ambiguo para conflito de titulares ou vínculo incerto"`
	HolderName             *string `json:"holder_name" jsonschema_description:"Nome completo legível do aluno/titular escolhido, transcrito sem inventar ou completar; vazio se responsável, ausente, ilegível ou ambíguo; nunca filiação, médico ou signatário"`
	HolderCPF              *string `json:"holder_cpf" jsonschema_description:"CPF integral legível exclusivamente do mesmo aluno/titular do nome, preservando zeros; vazio se ausente ou duvidoso; nunca CPF de pai, mãe, responsável ou profissional"`
	RecordNumber           *string `json:"record_number" jsonschema_description:"Número integral legível do prontuário do mesmo aluno/titular, rotulado no documento, preservando zeros; vazio se ausente ou duvidoso; não usar RG, matrícula, data, CRM, CNES ou número de registro de terceiros"`
	IdentificationEvidence *string `json:"identification_evidence" jsonschema_description:"Trecho curto transcrito do campo que comprova o nome e sua relação com o titular, indicando a página; inclua rótulos de CPF/prontuário quando usados; vazio se não identificado; não incluir dados de terceiros"`
}
type OpenAI struct {
	Client   *openai.Client
	sdkKey   string
	modelSDK string
}

func NewOpenAI(m, sk string) (*OpenAI, error) {
	if strings.TrimSpace(sk) == "" {
		return nil, errors.New("OpenAI API key não informada")
	}

	if strings.TrimSpace(m) == "" {
		return nil, errors.New("modelo OpenAI não informado")
	}

	client := openai.NewClient(
		option.WithAPIKey(sk),
	)

	return &OpenAI{
		Client:   &client,
		sdkKey:   sk,
		modelSDK: m,
	}, nil
}

func (open *OpenAI) AnalyzePDF(
	ctx context.Context,
	file *os.File,
) (*domain.ParametersPDF, error) {
	if file == nil {
		return nil, errors.New("arquivo PDF não informado")
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Println("Error closing file: " + err.Error())
		}
	}()

	fmt.Printf("[PDF %q] Enviando PDF para a OpenAI.\n", file.Name())
	fileID, err := open.uploadPDF(ctx, file)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[PDF %q] Upload concluído. Solicitando classificação e aguardando resposta da IA.\n", file.Name())
	model, err := open.classifyPDF(ctx, fileID)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[PDF %q] Resposta da IA recebida e interpretada.\n", file.Name())
	return model, nil
}

func (open *OpenAI) uploadPDF(ctx context.Context, file *os.File) (string, error) {
	if file == nil {
		return "", errors.New("arquivo PDF não informado")
	}

	uploadedFile, err := open.Client.Files.New(
		ctx,
		openai.FileNewParams{
			File:    file,
			Purpose: openai.FilePurposeUserData,
		},
	)

	if err != nil {
		return "", fmt.Errorf(
			"erro ao enviar o arquivo %q para OpenAI: %w",
			file.Name(),
			err,
		)
	}

	return uploadedFile.ID, nil
}

func (open *OpenAI) classifyPDF(
	ctx context.Context,
	fileID string,
) (*domain.ParametersPDF, error) {

	schema, err := generateSchema[inputDTO]()
	if err != nil {
		return nil, err
	}

	content := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentUnionParam{
			OfInputFile: &responses.ResponseInputFileParam{
				FileID: openai.String(fileID),
				Detail: responses.ResponseInputFileDetailHigh,
			},
		},

		responses.ResponseInputContentParamOfInputText("Analise todas as páginas deste PDF e retorne a classificação e a identificação conforme o schema."),
	}

	response, err := open.Client.Responses.New(
		ctx,
		responses.ResponseNewParams{
			Model:        responses.ResponsesModel(open.modelSDK),
			Instructions: openai.String(classificationInstructions),

			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: responses.ResponseInputParam{
					responses.ResponseInputItemParamOfMessage(
						content,
						responses.EasyInputMessageRoleUser,
					),
				},
			},

			Text: responses.ResponseTextConfigParam{
				Format: responses.ResponseFormatTextConfigUnionParam{
					OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
						Name:        "pdf_classification",
						Description: openai.String("Tipo do documento, formulário em branco e identificação segura do aluno/titular"),
						Schema:      schema,
						Strict:      openai.Bool(true),
					},
				},
			},
		},
	)

	if err != nil {
		return nil, fmt.Errorf(
			"erro ao analisar PDF com OpenAI: %w",
			err,
		)
	}

	if response.Status != responses.ResponseStatusCompleted {
		return nil, fmt.Errorf("análise da OpenAI não concluída: status %q, motivo %q", response.Status, response.IncompleteDetails.Reason)
	}
	for _, item := range response.Output {
		if item.Type == "message" {
			for _, content := range item.AsMessage().Content {
				if content.Type == "refusal" {
					return nil, errors.New("OpenAI recusou a análise do documento")
				}
			}
		}
	}

	return decodeClassification(response.OutputText())
}

// Mesmo com strict=true, validamos o transporte e as regras de domínio antes de mover o PDF.
func decodeClassification(text string) (*domain.ParametersPDF, error) {
	var dto inputDTO
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dto); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta da OpenAI: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("resposta da OpenAI deve conter somente um objeto JSON")
	}
	if dto.ClassificationName == nil || dto.IsBlankForm == nil || dto.HolderRole == nil ||
		dto.IdentificationStatus == nil || dto.HolderName == nil || dto.HolderCPF == nil ||
		dto.RecordNumber == nil || dto.IdentificationEvidence == nil {
		return nil, errors.New("resposta da OpenAI contém campos obrigatórios ausentes ou nulos")
	}
	if strings.TrimSpace(*dto.HolderRole) == "" || strings.TrimSpace(*dto.IdentificationStatus) == "" {
		return nil, errors.New("resposta da OpenAI contém papel ou status de identificação vazio")
	}
	result := &domain.ParametersPDF{
		ClassificationName:     *dto.ClassificationName,
		IsBlankForm:            *dto.IsBlankForm,
		HolderRole:             *dto.HolderRole,
		IdentificationStatus:   *dto.IdentificationStatus,
		HolderName:             *dto.HolderName,
		HolderCPF:              *dto.HolderCPF,
		RecordNumber:           *dto.RecordNumber,
		IdentificationEvidence: *dto.IdentificationEvidence,
	}
	return result.Normalize()
}

func generateSchema[T any]() (map[string]any, error) {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}

	var value T

	schema := reflector.Reflect(value)

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar schema: %w", err)
	}

	var rawSchema map[string]json.RawMessage

	if err := json.Unmarshal(data, &rawSchema); err != nil {
		return nil, fmt.Errorf("erro ao converter schema: %w", err)
	}

	result := make(map[string]any, len(rawSchema))

	for key, value := range rawSchema {
		result[key] = value
	}

	return result, nil
}
