package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const help = `
Usage: container [-h] [install deploy-file] [uninstall deploy-file] [status]
Configure the basic functions of containers.
Application_context: Users can use the app images to create containers by executing the container 
install container app-name command. The app images are download into xxx/xxx directory. 
The app images file will be deleted after setup.
Users can delete the containers by executing container uninstall container.
Users can get the status of containers by executing container status command. 
IP address, CPU usage, RAM usage and storage usage can be get only when the container is in running status, otherwise, all the value will be 0.
The command can be executed in any directory.
Commands:
-h			--help, show help information
install			--creating containers using deploy yaml file
uninstall		--uninstall the specified container using deploy yaml file
status			--show status information of containers
list			--show all containers

Parameters:
deploy-file		--deploy yaml file.
app-name		--Specify the app image file name in the container,string format.
`

type dockerStat struct {
	Container string
	Name      string
	Memory    string
	CPUPerc   string
	MemPerc   string
	BlockIO   string
}

func main() {
	len := len(os.Args)
	//fmt.Println(len, ",", os.Args)

	if len < 2 {
		fmt.Println(help)
		return
	}

	switch os.Args[1] {
	case "-h":
		fmt.Println(help)
	case "install":
		if len < 3 {
			fmt.Println(help)
		} else {
			kubeInstall(os.Args[2])
		}
	case "uninstall":
		if len < 3 {
			fmt.Println(help)
		} else {
			kubeUninstall(os.Args[2])
		}
	case "status":
		if len < 3 {
			fmt.Println(help)
		} else {
			kubeStatus(os.Args[2])
		}
	case "list":
		kubeList()
	default:
		fmt.Println(help)
	}
}

func execBashCmd(bash string) (string, error) {
	cmd := exec.Command("/bin/bash", "-c", bash)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}

func kubeInstall(name string) error {
	fmt.Println("It will take some time to install, please wait a moment.")
	res, err := execBashCmd("kubectl apply -f " + name)
	if err != nil {
		fmt.Println(res, ",", err)
		return err
	}
	fmt.Println(res)
	return err
}

func kubeUninstall(name string) error {
	fmt.Println("It will take some time to uninstall, please wait a moment.")
	res, err := execBashCmd("kubectl delete -f " + name)
	if err != nil {
		fmt.Println(res, ",", err)
		return err
	}
	fmt.Println(res)
	return err
}

func kubeStatus(name string) error {
	fmt.Println("It will take some time to get status, please wait a moment.")
	res, err := execBashCmd("kubectl describe pod " + name)
	if err != nil {
		fmt.Println(res, ",", err)
		return err
	}
	//fmt.Println(res)

	var ip string
	var container string
	var status string
	strList := strings.Split(res, "\n")
	for _, v := range strList {
		if idx := strings.Index(v, "IP:"); idx > -1 {
			ip = strings.TrimSpace(v[idx+len("IP:"):])
		}
		if idx := strings.Index(v, "Container ID:"); idx > -1 {
			container = strings.TrimSpace(v[idx+len("Container ID:"):])
		}
	}

	if idx := strings.Index(container, "docker://"); idx > -1 {
		container = strings.TrimSpace(container[idx+len("docker://"):])
	}
	res, err = execBashCmd(`kubectl get pod ` + name + ` | awk '{print $3}'`)
	if err != nil {
		fmt.Println(err)
	} else {
		statList := strings.Split(res, "\n")
		if len(statList) > 1 {
			status = statList[1]
		}
	}

	ret, err := execBashCmd(`docker stats ` + container + ` --no-stream --format "{\"container\":\"{{ .Container }}\",\"name\":\"{{ .Name }}\",
	\"memory\":\"{{ .MemUsage }}\",\"cpuperc\":\"{{ .CPUPerc }}\",\"memperc\":\"{{ .MemPerc }}\",\"blockio\":\"{{ .BlockIO }}\"}"`)

	ds := dockerStat{}
	err = json.Unmarshal([]byte(ret), &ds)
	if err != nil {
		//fmt.Println(err)
		//log.Println(ret)
	} else {
		//fmt.Println(ds)
	}

	fmt.Println("name:\t\t", name)
	fmt.Println("status:\t\t", status)
	fmt.Println("ip:\t\t", ip)
	fmt.Println("cpu-usage:\t", ds.CPUPerc)
	fmt.Println("memory:\t\t", ds.Memory)
	fmt.Println("memory-usage:\t", ds.MemPerc)
	fmt.Println("block io:\t", ds.BlockIO)

	return nil
}

func kubeList() error {
	fmt.Println("It will take some time to get list, please wait a moment.")
	res, err := execBashCmd("kubectl get pods")
	if err != nil {
		fmt.Println(res, ",", err)
		return err
	}
	fmt.Println(res)
	return err
}
