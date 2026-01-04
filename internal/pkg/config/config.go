package config

import (
	"cmp"
	"errors"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

type ParrotConfig struct {
	GiteaToken         *string `yaml:"gitea_token"`
	LLMEndpoint        *string `yaml:"llm_endpoint"`
	LLMToken           *string `yaml:"llm_token"`
	SystemPrompt       *string `yaml:"system_prompt"`
	UserPrompt         *string
	InsecureSkipVerify *bool    `yaml:"insecure_skip_tls_verify"`
	Port               *int     `yaml:"port"`
	LLMTimeout         *int     `yaml:"llm_timeout"`
	GiteaUsername      *string  `yaml:"gitea_username"`
	Model              *string  `yaml:"model"`
	Temperature        *float64 `yaml:"temperature"`
}

const (
	ErrMissingGiteaToken  = "gitea_token must not be nil"
	ErrMissingLLMEndpoint = "llm_endpoint must not be nil"
	ErrMissingLLMToken    = "llm_token must not be nil"
)

func (c *ParrotConfig) Parse(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.loadConfig(f)
}

func (c *ParrotConfig) loadConfig(configFile fs.File) error {
	tempConfig := new(ParrotConfig)

	err := yaml.NewDecoder(configFile).Decode(tempConfig)
	if err != nil {
		return err
	}

	c.GiteaToken = cmp.Or(tempConfig.GiteaToken, &[]string{os.Getenv("GITEA_TOKEN")}[0])
	if *c.GiteaToken == "" {
		return errors.New(ErrMissingGiteaToken)
	}

	c.LLMEndpoint = cmp.Or(tempConfig.LLMEndpoint, &[]string{os.Getenv("LLM_ENDPOINT")}[0])
	if *c.LLMEndpoint == "" {
		return errors.New(ErrMissingLLMEndpoint)
	}

	c.LLMToken = cmp.Or(tempConfig.LLMEndpoint, &[]string{os.Getenv("LLM_TOKEN")}[0])
	if *c.LLMToken == "" {
		return errors.New(ErrMissingLLMToken)
	}

	c.SystemPrompt = cmp.Or(tempConfig.SystemPrompt, &[]string{defaultSystemPrompt}[0])

	c.InsecureSkipVerify = cmp.Or(tempConfig.InsecureSkipVerify, &[]bool{false}[0])

	if tempConfig.Port != nil && (*tempConfig.Port > 0 && *tempConfig.Port < 65535) {
		c.Port = tempConfig.Port
	} else {
		c.Port = &[]int{defaultPort}[0]
	}

	if tempConfig.LLMTimeout != nil {
		switch {
		case *tempConfig.LLMTimeout > 0:
			c.LLMTimeout = tempConfig.LLMTimeout
		default:
			c.LLMTimeout = &[]int{defaultLLMTimeout}[0]
		}
	} else {
		c.LLMTimeout = &[]int{defaultLLMTimeout}[0]
	}

	c.GiteaUsername = cmp.Or(tempConfig.GiteaUsername, &[]string{""}[0])

	c.Model = cmp.Or(tempConfig.Model, &[]string{defaultLLMModel}[0])

	c.UserPrompt = &[]string{defaultUserPrompt}[0]

	c.Temperature = cmp.Or(tempConfig.Temperature, &[]float64{defaultTemperature}[0])

	return nil
}
