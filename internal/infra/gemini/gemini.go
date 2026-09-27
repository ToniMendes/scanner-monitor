package gemini

import (
	"context"
	"fmt"
	"scanner-monitor/internal/configs"
	"time"

	"google.golang.org/genai"
)

type GeminiIA struct {
	apiKey string
}

func NewGeminiIA() *GeminiIA {
	return &GeminiIA{
		apiKey: configs.Env.GeminiAPIKey,
	}
}

const prompt = "quero um lorem ipsum com 10 paragrafos"

func (g *GeminiIA) InitGemini() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: g.apiKey,
	})

	if err != nil {
		return err
	}

	stream := client.Models.GenerateContentStream(ctx, "gemini-3.8-flash", genai.Text(prompt), nil)

	for resp, err := range stream {
		if err != nil {
			return err
		}

		fmt.Println(resp.Text())
	}

	return nil
}
