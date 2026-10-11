package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"scanner-monitor/internal/domain"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type inputDTO struct {
	ClassificationName string `json:"classification_name" jsonschema:"enum=atestado,enum=carteira_vacinacao,enum=documento_aluno,enum=formulario,enum=outro" jsonschema_description:"Classificação principal identificada para o documento"`
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

	fileID, err := open.uploadPDF(ctx, file)
	if err != nil {
		return nil, err
	}

	model, err := open.classifyPDF(ctx, fileID)
	if err != nil {
		return nil, err
	}

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

	const prompt = `
Analise o documento PDF fornecido.

Identifique qual é o tipo principal do documento.

Caso não encontre algo especifico no schema, coloque em outros.
`

	schema, err := generateSchema[inputDTO]()
	if err != nil {
		return nil, err
	}

	content := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentUnionParam{
			OfInputFile: &responses.ResponseInputFileParam{
				FileID: openai.String(fileID),
			},
		},

		responses.ResponseInputContentParamOfInputText(prompt),
	}

	response, err := open.Client.Responses.New(
		ctx,
		responses.ResponseNewParams{
			Model: responses.ResponsesModel(open.modelSDK),

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
						Description: openai.String("Classificação do documento PDF"),
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

	var dto inputDTO

	if err := json.Unmarshal(
		[]byte(response.OutputText()),
		&dto,
	); err != nil {
		return nil, fmt.Errorf(
			"erro ao decodificar resposta da OpenAI: %w",
			err,
		)
	}

	result, err := domain.NewParametersPDF(
		dto.ClassificationName,
	)
	if err != nil {
		return nil, err
	}

	return result, nil
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
