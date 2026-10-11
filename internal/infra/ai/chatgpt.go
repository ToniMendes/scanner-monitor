package ai

import (
	"errors"
	"scanner-monitor/internal/config"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type OpenAI struct {
	sdkKey string
	model  string
}

func NewOpenAI() (*OpenAI, error) {
	return &OpenAI{
		sdkKey: config.Env.GPTAPIKey,
		model:  "gpt-5.6-luna",
	}, nil
}

func (open *OpenAI) openaiClient() (openai.Client, error) {
	if strings.TrimSpace(open.sdkKey) == "" {
		return openai.Client{}, errors.New("OpenAI API key não informada")
	}

	if strings.TrimSpace(open.model) == "" {
		return openai.Client{}, errors.New("modelo OpenAI não informado")
	}

	client := openai.NewClient(
		option.WithAPIKey(open.sdkKey),
	)

	return client, nil
}



func (open *OpenAI) AnalyzePDF(text string) (openai.Client, error) {

}
