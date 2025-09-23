package config

import (
	"errors"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

type ParrotConfig struct {
	GiteaToken         *string `yaml:"gitea_token"`
	OpenAIEndpoint     *string `yaml:"openai_endpoint"`
	OpenAIToken        *string `yaml:"openai_token"`
	ReviewPrompt       *string `yaml:"review_prompt"`
	InsecureSkipVerify *bool   `yaml:"insecure_skip_tls_verify"`
	Port               *int    `yaml:"port"`
	LLMTimeout         *int    `yaml:"llm_timeout"`
	GiteaUsername      *string `yaml:"gitea_username"`
	Model              *string `yaml:"model"`
}

const (
	ErrMissingGiteaToken     = "gitea_token must not be nil"
	ErrMissingOpenAIEndpoint = "openai_endpoint must not be nil"
	ErrMissingOpenAIToken    = "openai_token must not be nil"
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

	if tempConfig.GiteaToken == nil {
		return errors.New(ErrMissingGiteaToken)
	}
	c.GiteaToken = tempConfig.GiteaToken

	if tempConfig.OpenAIEndpoint == nil {
		return errors.New(ErrMissingOpenAIEndpoint)
	}
	c.OpenAIEndpoint = tempConfig.OpenAIEndpoint

	if tempConfig.OpenAIToken == nil {
		return errors.New(ErrMissingOpenAIToken)
	}
	c.OpenAIToken = tempConfig.OpenAIToken

	if tempConfig.ReviewPrompt == nil {
		c.ReviewPrompt = &[]string{defaultReviewPrompt}[0]
	} else {
		c.ReviewPrompt = tempConfig.ReviewPrompt
	}

	if tempConfig.InsecureSkipVerify != nil {
		c.InsecureSkipVerify = tempConfig.InsecureSkipVerify
	} else {
		c.InsecureSkipVerify = &[]bool{false}[0]
	}

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

	if tempConfig.GiteaUsername != nil {
		c.GiteaUsername = tempConfig.GiteaUsername
	} else {
		c.GiteaUsername = &[]string{""}[0]
	}

	if tempConfig.Model != nil {
		c.Model = tempConfig.Model
	} else {
		c.Model = &[]string{"gemini-2.5-pro"}[0]
	}

	return nil
}
