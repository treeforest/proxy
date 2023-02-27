package conf

import (
	"gopkg.in/yaml.v3"
	"io/ioutil"
)

func Load(path string) *Config {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		panic(err)
	}

	return &cfg
}

type Config struct {
	ServerAddress string `yaml:"serverAddress"`
	HttpBaseURL   string `yaml:"httpBaseURL"`
	WsBaseURL     string `yaml:"wsBaseURL"`
	EnableTLS     bool   `yaml:"enableTLS"`
}

func (c *Config) String() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}
