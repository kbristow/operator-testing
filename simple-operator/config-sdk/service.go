package config_sdk

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

type ConfigService interface {
	GetConfig(string) (string, error)
}

type Service struct {
	Url string
}

func newService() ConfigService {
	url := os.Getenv("CONFIG_URL")
	return Service{Url: url}
}

var ConfigServiceFactoryFunction = newService

func NewConfigService() ConfigService {
	return ConfigServiceFactoryFunction()
}

func (s Service) GetConfig(configName string) (string, error) {
	httpClient := http.DefaultClient

	getConfigRequest, _ := http.NewRequest("GET", fmt.Sprintf("%s/%s", s.Url, configName), nil)

	response, err := httpClient.Do(getConfigRequest)

	if err != nil {
		return "", err
	}

	if response.StatusCode != 200 {
		err := fmt.Errorf("failed to get config with error result: %d", response.StatusCode)
		return "", err
	}

	configBody, err := ioutil.ReadAll(response.Body)

	return string(configBody), err
}
