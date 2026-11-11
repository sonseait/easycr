package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Data, Listen string
}

func Parse(args []string) (Config, error) {
	c := Config{Data: "./data", Listen: "0.0.0.0:5000"}
	if len(args) != 0 {
		return c, fmt.Errorf("configuration arguments are not supported")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return c, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	c.Listen = "0.0.0.0:" + strconv.Itoa(n)
	return c, nil
}
