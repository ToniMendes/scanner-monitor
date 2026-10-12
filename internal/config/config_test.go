package config

import (
	"github.com/spf13/viper"
	"os"
	"testing"
)

func TestStartViper(t *testing.T) {
	cases := []struct {
		name, content    string
		missing, wantErr bool
	}{
		{"valid", "CHATGPT_SDK_KEY=test-key\nMODEL_OPENAI=test-model\nFILE_PATH_BASE=C:\\Scanner\\entrada\nDIR_PATH_BASE=C:\\Scanner\\saida\n", false, false},
		{"missing file", "", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			for _, key := range []string{"CHATGPT_SDK_KEY", "MODEL_OPENAI", "FILE_PATH_BASE", "DIR_PATH_BASE"} {
				t.Setenv(key, "")
			}
			if !tc.missing {
				if err := os.WriteFile(".env", []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := StartViper()
			if (err != nil) != tc.wantErr {
				t.Fatal(err)
			}
			if err == nil && (Env.OpenaiSDKKey != "test-key" || Env.ModelOpenAI != "test-model") {
				t.Fatalf("%+v", Env)
			}
		})
	}
}
func TestRequiredConfiguration(t *testing.T) {
	values := map[string]string{"CHATGPT_SDK_KEY": "key", "MODEL_OPENAI": "model", "FILE_PATH_BASE": "entrada", "DIR_PATH_BASE": "saida"}
	for _, key := range []string{"CHATGPT_SDK_KEY", "MODEL_OPENAI", "FILE_PATH_BASE", "DIR_PATH_BASE"} {
		for _, value := range []string{"", "   "} {
			t.Run(key+value, func(t *testing.T) {
				t.Chdir(t.TempDir())
				content := ""
				for k, v := range values {
					t.Setenv(k, "")
					if k == key {
						v = value
					}
					content += k + "=" + v + "\n"
				}
				if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				if err := StartViper(); err == nil {
					t.Fatalf("%s accepted empty value", key)
				}
			})
		}
	}
}
func TestEnvironmentOverridesAndReload(t *testing.T) {
	t.Chdir(t.TempDir())
	content := "CHATGPT_SDK_KEY=file-key\nMODEL_OPENAI=file-model\nFILE_PATH_BASE=entrada\nDIR_PATH_BASE=saida\n"
	if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHATGPT_SDK_KEY", "env-key")
	t.Setenv("MODEL_OPENAI", "env-model")
	t.Setenv("FILE_PATH_BASE", "env-input")
	t.Setenv("DIR_PATH_BASE", "env-output")
	if err := StartViper(); err != nil {
		t.Fatal(err)
	}
	if Env.OpenaiSDKKey != "env-key" || Env.ModelOpenAI != "env-model" || Env.FilePathBase != "env-input" || Env.DirPathBase != "env-output" {
		t.Fatal("environment override failed")
	}
	t.Setenv("MODEL_OPENAI", "")
	if err := StartViper(); err != nil {
		t.Fatal(err)
	}
	if Env.ModelOpenAI != "file-model" {
		t.Fatal("stale configuration")
	}
}
