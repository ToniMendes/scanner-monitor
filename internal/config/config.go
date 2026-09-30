// Package config loads application configuration from environment and .env files.
package config

import (
	"fmt"

	"github.com/spf13/viper"

)

type Config struct {
	GeminiAPIKey string `mapstructure:"GEMINI_API_KEY"`
	ChatGPTAPIKey string `mapstructure:"CHATGPT_API_KEY"`
}

var Env *Config

func StartViper() error {
	viper.SetConfigName(".env") // Nome do arquivo sem a extensão
	viper.SetConfigType("env")  // Formato do arquivo
	viper.AddConfigPath(".")    // Caminho (diretório raiz do projeto)

	// Permite que variáveis de ambiente do SO sobrescrevam as do arquivo
	viper.AutomaticEnv()

	// 1. Lê o arquivo de configuração
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("erro ao ler o arquivo .env: %w", err)
	}

	// 2. Instancia a struct e faz o Unmarshal dos dados

	if err := viper.Unmarshal(&Env); err != nil {
		return fmt.Errorf("erro ao decodificar as configurações para a struct: %w", err)
	}

	// Retorna o ponteiro para a struct populada
	return nil
}
