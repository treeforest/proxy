package conf

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BaseURL     string `yaml:"baseURL"`
	RpcAddress  string `yaml:"rpcAddress"`
	HttpAddress string `yaml:"httpAddress"`
	EnableTLS   bool   `yaml:"enableTLS"` // 是否开启tls连接
	BoltDBPath  string `yaml:"boltDBPath"`
	Logger      Logger `yaml:"logger"`
}

type Logger struct {
	Type     int    `yaml:"type"`     // 日志输出类型, 1 控制台输出; 2 文件输出
	Level    int    `yaml:"level"`    // 日志等级, 1 DEBUG; 2 INFO; 3 WARN; 4 ERROR; 5 FATAL
	Path     string `yaml:"path"`     // 日志文件输出路径
	Capacity uint32 `yaml:"capacity"` // 日志文件大小上限，用于分片
}

func Default() *Config {
	return &Config{
		BaseURL:     "http://127.0.0.1:9090",
		RpcAddress:  "0.0.0.0:9091",
		HttpAddress: "0.0.0.0:9090",
		EnableTLS:   true,
		BoltDBPath:  "my.db",
	}
}

func Load(path string) *Config {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}

	cfg := new(Config)

	if err = yaml.Unmarshal(data, cfg); err != nil {
		panic(err)
	}

	switch cfg.Logger.Type {
	case 1, 2:
	default:
		fmt.Printf("logger\n\t- type:%d # 日志类型不符合规范\n", cfg.Logger.Type)
	}

	switch cfg.Logger.Level {
	case 0, 1, 2, 3, 4:
	default:
		fmt.Printf("logger\n\t- level:%d # 日志级别不符合规范\n", cfg.Logger.Level)
	}

	if cfg.Logger.Type == 2 {
		if cfg.Logger.Path == "" {
			fmt.Printf("logger\n\t- level:%s # 日志文件路径不能为空\n", cfg.Logger.Path)
		}

		if cfg.Logger.Capacity == 0 {
			fmt.Printf("logger\n\t- level:%d # 日志文件大小必须大于0\n", cfg.Logger.Capacity)
		}
	}

	return cfg
}

func (c *Config) String() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}

func (c *Config) Write(path string) {
	data, _ := yaml.Marshal(c)
	_ = ioutil.WriteFile(path, data, 0666)
}
