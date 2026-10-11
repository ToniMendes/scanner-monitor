// Package config loads application configuration from environment and .env files.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	OpenaiSDKKey string `mapstructure:"CHATGPT_SDK_KEY"`
	ModelOpenAI  string `mapstructure:"MODEL_OPENAI"`
	FilePathBase string `mapstructure:"FILE_PATH_BASE"`
	DirPathBase  string `mapstructure:"DIR_PATH_BASE"`
}

var Env *Config

func StartViper() error {
	viper := viper.New()
	viper.SetConfigName(".env") // Nome do arquivo sem a extensão
	viper.SetConfigType("env")  // Formato do arquivo
	viper.AddConfigPath(".")    // Caminho (diretório raiz do projeto)

	// Permite que variáveis de ambiente do SO sobrescrevam as do arquivo
	viper.AutomaticEnv()
	for _, key := range []string{"CHATGPT_SDK_KEY", "MODEL_OPENAI", "FILE_PATH_BASE", "DIR_PATH_BASE"} {
		if err := viper.BindEnv(key); err != nil {
			return err
		}
	}

	// 1. Lê o arquivo de configuração
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("erro ao ler o arquivo .env: %w", err)
	}

	// 2. Instancia a struct e faz o Unmarshal dos dados

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("erro ao decodificar as configurações para a struct: %w", err)
	}
	for _, item := range []struct{ key, value string }{
		{"CHATGPT_SDK_KEY", cfg.OpenaiSDKKey}, {"MODEL_OPENAI", cfg.ModelOpenAI},
		{"FILE_PATH_BASE", cfg.FilePathBase}, {"DIR_PATH_BASE", cfg.DirPathBase},
	} {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%s é obrigatória", item.key)
		}
	}
	Env = &cfg

	// Retorna o ponteiro para a struct populada
	return nil
}
