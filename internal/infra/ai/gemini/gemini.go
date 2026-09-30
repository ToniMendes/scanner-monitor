// Package ai provides integration with Google's Gemini API.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"scanner-monitor/internal/config"
	"time"

	"google.golang.org/genai"

)

type inputDTO struct {
	DirName string `json:"dirname"`
}

type GeminiAI struct {
	apiKey         string
	gemini3_8flash string
	gemini3_1pro   string
}

func NewGeminiAI() *GeminiAI {
	return &GeminiAI{
		apiKey:         config.Env.GeminiAPIKey,
		gemini3_8flash: "models/gemini-3.8-flash",
		gemini3_1pro:   "models/gemini-3.1-pro-preview",
	}
}

func (g *GeminiAI) startGemini(ctx context.Context) (*genai.Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: g.apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cliente Gemini: %w", err)
	}

	return client, nil
}

func (g *GeminiAI) ReadPath(pdfChan <-chan string, mkdirNameChan chan<- string) error {
	defer close(mkdirNameChan)

	if pdfChan == nil {
		return fmt.Errorf("nenhum caminho de pdf recebido")
	}

	for pdfPath := range pdfChan {
		fmt.Println("PDF recebido, caminho: ", pdfPath)

		file, err := os.Open(pdfPath)
		if err != nil {
			return fmt.Errorf("erro ao abrir o arquivo pdf: %w", err)
		}

		err = func() error {
			defer file.Close()

			dto, err := g.uploadPDFToGemini(file)
			if err != nil {
				return fmt.Errorf("erro ao enviar PDF para Gemini: %w", err)
			}

			mkdirNameChan <- dto.DirName
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *GeminiAI) uploadPDFToGemini(file *os.File) (*inputDTO, error) {
	var dto inputDTO

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client, err := g.startGemini(ctx)
	if err != nil {
		return &dto, fmt.Errorf("erro ao iniciar Gemini: %w", err)
	}

	uploadedFile, err := client.Files.Upload(ctx, file, &genai.UploadFileConfig{
		MIMEType: "application/pdf",
	})
	if err != nil {
		return &dto, fmt.Errorf("erro ao fazer upload na File API: %w", err)
	}

	// Remove o arquivo temporário dos servidores do Gemini ao terminar
	defer func() {
		_, err := client.Files.Delete(ctx, uploadedFile.Name, nil)
		if err != nil {
			log.Fatal("erro ao deletar o arquivo pdf do servidor.")
		}
	}()

	// 2. Prepara a chamada para o modelo ler o PDF enviado
	prompt := "Analise este documento PDF e forneça um nome de pasta padronizado (dirname) baseado nas informações presentes. exemplo (NOME_CPF ou RG ou PRONTUARIO),"

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"dirname": {
					Type:        genai.TypeString,
					Description: "Sugestão de nome de pasta padronizado para o aluno (ex: Nome_CPF) Ou caso seja um pdf diferente crie com um nome de categorize ele.",
				},
			},
			Required: []string{"dirname"},
		},
	}

	resp, err := client.Models.GenerateContent(ctx, g.gemini3_8flash, []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: prompt},
				{FileData: &genai.FileData{
					FileURI:  uploadedFile.URI,
					MIMEType: uploadedFile.MIMEType,
				}},
			},
		},
	}, config)
	if err != nil {
		return &dto, fmt.Errorf("erro ao gerar resposta do Gemini: %w", err)
	}

	if err := json.Unmarshal([]byte(resp.Text()), &dto); err != nil {
		return &dto, fmt.Errorf("erro ao converter resposta JSON para struct Student")
	}

	// 3. Imprime ou processa o texto retornado pelo Gemini
	fmt.Printf("\n--- Resultado do PDF: ---\n%s\n\n", resp.Text())

	return &dto, nil
}
