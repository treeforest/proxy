package main

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"net/http"
	"os/exec"
)

func main() {
	gin.SetMode(gin.DebugMode)

	engine := gin.Default()
	engine.POST("/hello", func(c *gin.Context) {
		c.String(http.StatusOK, "world")
	})
	engine.GET("/hi", func(c *gin.Context) {
		name, _ := c.GetQuery("name")
		c.Header("cookie", "1234556")
		c.JSON(http.StatusOK, gin.H{
			"say": "hi " + name,
		})
	})
	engine.GET("/cmd", func(c *gin.Context) {
		buf := bytes.NewBuffer(nil)
		cmdStr, _ := c.GetQuery("cmd")
		cmd := exec.Command(cmdStr)
		cmd.Stdout = buf
		_ = cmd.Run()

		result := buf.String()

		c.JSON(http.StatusOK, gin.H{
			"result": result,
		})
	})
	engine.Run(":9010")
}
