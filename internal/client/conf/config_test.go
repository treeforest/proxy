package conf

import (
	"io/ioutil"
	"testing"
)

func Test_Config(t *testing.T) {
	c := Config{}
	ioutil.WriteFile("config.yaml", []byte(c.String()), 0666)
}
