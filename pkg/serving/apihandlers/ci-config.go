package apihandlers

import (
	"io/ioutil"

	"github.com/ghodss/yaml"
	"github.com/tnozicka/ocp-code-tools/pkg/api"
)

func GetCIConfig(path string) (*api.Config, error) {
	rawData, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	c := &api.Config{}
	err = yaml.Unmarshal(rawData, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}
