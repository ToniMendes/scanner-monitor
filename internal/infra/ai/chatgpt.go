// Package openai provides integration with the OpenAI API.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"scanner-monitor/internal/config"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

)

type inputDTO struct {
	DirName string `json:"dirname"`
}

type OpenAI struct {
	apiKey string
	model  string
}

func NewOpenAI() *OpenAI {
	return &OpenAI{
		apiKey: config.Env.ChatGPTAPIKey,

		// Barato e suficiente para esse tipo de extração/classificação.
		model: "gpt-5.6-luna",

		// Se quiser mais qualidade:
		// model: "gpt-5.6",
	}
}

func (o *OpenAI) startOpenAI() *openaisdk.Client {
	client := openaisdk.NewClient(
		option.WithAPIKey(o.apiKey),
	)
	return &client
}

func (o *OpenAI) ReadPath(
	pdfChan <-chan string,
	mkdirNameChan chan<- string,
) error {

	defer close(mkdirNameChan)

	if pdfChan == nil {
		return fmt.Errorf("nenhum caminho de PDF recebido")
	}

	for pdfPath := range pdfChan {
		fmt.Println("PDF recebido, caminho:", pdfPath)

		file, err := os.Open(pdfPath)
		if err != nil {
			return fmt.Errorf(
				"erro ao abrir o arquivo PDF %q: %w",
				pdfPath,
				err,
			)
		}

		err = func() error {
			defer file.Close()

			dto, err := o.uploadPDFToOpenAI(file)
			if err != nil {
				return fmt.Errorf(
					"erro ao enviar PDF para OpenAI: %w",
					err,
				)
			}

			mkdirNameChan <-dto.DirName

			return nil
		}()

		if err != nil {
			return err
		}
	}

	return nil
}

func (o *OpenAI) uploadPDFToOpenAI(
	file *os.File,
) (*inputDTO, error) {

	var dto inputDTO

	ctx, cancel := context.WithTimeout(
		context.Background(),
		120*time.Second,
	)
	defer cancel()

	client := o.startOpenAI()

	// ---------------------------------------------------
	// 1. Faz upload do PDF
	// ---------------------------------------------------

	uploadedFile, err := client.Files.New(
		ctx,
		openaisdk.FileNewParams{
			File:    file,
			Purpose: openaisdk.FilePurposeUserData,
		},
	)

	if err != nil {
		return nil, fmt.Errorf(
			"erro ao fazer upload do PDF: %w",
			err,
		)
	}

	fmt.Println(
		"Arquivo enviado para OpenAI:",
		uploadedFile.ID,
	)

	// ---------------------------------------------------
	// Remove o arquivo da OpenAI quando terminar
	// ---------------------------------------------------

	defer func() {
		deleteCtx, cancel := context.WithTimeout(
			context.Background(),
			20*time.Second,
		)
		defer cancel()

		_, err := client.Files.Delete(
			deleteCtx,
			uploadedFile.ID,
		)

		if err != nil {
			log.Printf(
				"erro ao deletar arquivo %s da OpenAI: %v",
				uploadedFile.ID,
				err,
			)
		}
	}()

	// ---------------------------------------------------
	// 2. Prompt
	// ---------------------------------------------------

	prompt := `
Analise cuidadosamente o documento PDF enviado.

Seu objetivo é criar um nome de pasta padronizado
baseado nas informações presentes no documento.

Prioridade:

1. Se existir nome da pessoa e CPF:
   NOME_CPF

2. Se existir nome e RG:
   NOME_RG

3. Se existir nome e número de prontuário:
   NOME_PRONTUARIO

4. Caso seja outro tipo de documento,
   crie um nome curto que represente sua categoria.

Regras:

- utilize apenas o nome necessário;
- substitua espaços por "_";
- não utilize barras;
- não utilize caminhos de diretório;
- não utilize caracteres inválidos de nome de arquivo;
- não escreva explicações;
- retorne somente o resultado solicitado no schema JSON.
`

	// ---------------------------------------------------
	// 3. JSON Schema
	// ---------------------------------------------------

	schema := map[string]any{
		"type": "object",

		"properties": map[string]any{
			"dirname": map[string]any{
				"type":        "string",
				"description": "Nome padronizado da pasta referente ao PDF.",
			},
		},

		"required": []string{
			"dirname",
		},

		"additionalProperties": false,
	}

	// ---------------------------------------------------
	// 4. Envia PDF + prompt para o modelo
	// ---------------------------------------------------

	resp, err := client.Responses.New(
		ctx,
		responses.ResponseNewParams{
			Model: openaisdk.ResponsesModel(o.model),

			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: responses.ResponseInputParam{

					responses.ResponseInputItemParamOfMessage(
						responses.ResponseInputMessageContentListParam{

							// PDF
							{
								OfInputFile: &responses.ResponseInputFileParam{
									FileID: openaisdk.String(
										uploadedFile.ID,
									),
								},
							},

							// Prompt
							responses.ResponseInputContentParamOfInputText(
								prompt,
							),
						},

						responses.EasyInputMessageRoleUser,
					),
				},
			},

			// ------------------------------------------------
			// Structured Output
			// ------------------------------------------------

			Text: responses.ResponseTextConfigParam{
				Format: responses.ResponseFormatTextConfigUnionParam{

					OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
						Name: "pdf_directory_name",

						Description: openaisdk.String(
							"Nome padronizado da pasta correspondente ao PDF.",
						),

						Schema: schema,

						Strict: openaisdk.Bool(true),
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

	// ---------------------------------------------------
	// 5. Pega o JSON retornado
	// ---------------------------------------------------

	responseText := resp.OutputText()

	fmt.Printf(
		"\n--- Resultado do PDF ---\n%s\n\n",
		responseText,
	)

	// ---------------------------------------------------
	// 6. JSON -> struct Go
	// ---------------------------------------------------

	if err := json.Unmarshal(
		[]byte(responseText),
		&dto,
	); err != nil {

		return nil, fmt.Errorf(
			"erro ao converter resposta JSON para inputDTO: %w; resposta: %s",
			err,
			responseText,
		)
	}

	if dto.DirName == "" {
		return nil, fmt.Errorf(
			"OpenAI retornou dirname vazio",
		)
	}

	return &dto, nil
}
