package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"
)

func main() {
	fd, err := os.Open("D:\\Tools\\k8s_nodes\\jc.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer fd.Close()

	cont, err := ioutil.ReadAll(fd)
	if err != nil {
		fmt.Println(err)
		return
	}

	j, err := yaml.YAMLToJSON(cont)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Println(string(j))

	y, err := yaml.JSONToYAML(j)
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	fmt.Println(string(y))
}
