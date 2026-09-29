// Package ai provides integration with Google's Gemini API.
package ai

import (
	"context"
	"fmt"
	"log"
	"os"
	"scanner-monitor/internal/configs"
	"time"

	"google.golang.org/genai"

)

type GeminiAI struct {
	apiKey         string
	gemini3_8flash string
	gemini3_1pro   string
}

func NewGeminiAI() *GeminiAI {
	return &GeminiAI{
		apiKey:         configs.Env.GeminiAPIKey,
		gemini3_8flash: "models/gemini-3.8-flash",
		gemini3_1pro:   "models/gemini-3.1-pro-preview",
	}
}

func (g *GeminiAI) startGemini() (*genai.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: g.apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cliente Gemini: %w", err)
	}

	return client, nil
}

func (g *GeminiAI) ReadPath(pdfChan chan string) error {
	defer close(pdfChan)

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

			err = g.uploadPDFToGemini(file)
			if err != nil {
				return fmt.Errorf("erro ao enviar PDF para Gemini: %w", err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *GeminiAI) uploadPDFToGemini(file *os.File) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client, err := g.startGemini()
	if err != nil {
		return fmt.Errorf("erro ao iniciar Gemini: %w", err)
	}

	uploadedFile, err := client.Files.Upload(ctx, file, &genai.UploadFileConfig{
			MIMEType: "application/pdf",
	})
	if err != nil {
		return fmt.Errorf("erro ao fazer upload na File API: %w", err)
	}

	// Remove o arquivo temporário dos servidores do Gemini ao terminar
	defer func() {
		_, err := client.Files.Delete(ctx, uploadedFile.Name, nil)
		if err != nil {
			log.Fatal("erro ao deletar o arquivo pdf do servidor.")
		}
	}()

	// 2. Prepara a chamada para o modelo ler o PDF enviado
	prompt := "apenas diga oque é cada um dos pdf, nao precisa explicar nada, apenas diga oque eles são."

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
	}, nil)
	if err != nil {
		return fmt.Errorf("erro ao gerar resposta do Gemini: %w", err)
	}

	// 3. Imprime ou processa o texto retornado pelo Gemini
	fmt.Printf("\n--- Resultado do PDF: ---\n%s\n\n", resp.Text())

	return nil
}
