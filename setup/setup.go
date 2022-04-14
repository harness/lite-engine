// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package setup

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/sirupsen/logrus"
)

type InstanceInfo struct {
	osType string
}

func GetInstanceInfo() InstanceInfo {
	osType := runtime.GOOS
	return InstanceInfo{osType: osType}
}

func PrepareSystem() {
	instanceInfo := GetInstanceInfo()
	if !GitInstalled(instanceInfo) {
		installGit(instanceInfo)
	}
	if !DockerInstalled(instanceInfo) {
		installDocker(instanceInfo)
	}
}

const windowsString = "windows"
const osxString = "darwin"

func GitInstalled(instanceInfo InstanceInfo) (installed bool) {
	logrus.Infoln("checking git is installed")
	switch instanceInfo.osType {
	case windowsString:
		logrus.Infoln("windows: we should check git installation here")
	default:
		_, err := os.Stat("/usr/bin/git")
		if os.IsNotExist(err) {
			logrus.Infoln("git is not installed")
			return false
		}
	}
	logrus.Infoln("git is installed")
	return true
}

func DockerInstalled(instanceInfo InstanceInfo) (installed bool) {
	logrus.Infoln("checking docker is installed")
	switch instanceInfo.osType {
	case windowsString:
		logrus.Infoln("windows: we should check docker installation here")
	case osxString:
		cmd := exec.Command("/usr/local/bin/docker", "ps")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return false
		}
	default:
		cmd := exec.Command("docker", "ps")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return false
		}
	}
	logrus.Infoln("docker is installed")
	return true
}

func GetLiteEngineLog(instanceInfo InstanceInfo) string {
	switch instanceInfo.osType {
	case "linux":
		content, err := os.ReadFile("/var/log/lite-engine.log")
		if err != nil {
			return "no log file at /var/log/lite-engine.log"
		}
		return string(content)
	default:
		return "no log file"
	}
}

func ensureChocolatey() {
	const windowsInstallChoco = "Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://chocolatey.org/install.ps1')) " //nolint:lll
	cmd := exec.Command("choco", "-h")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		cmd := exec.Command(windowsInstallChoco)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		chocoErr := cmd.Run()
		if chocoErr != nil {
			logrus.Errorf("failed to install chocolatey: %s", chocoErr)
		}
	}
}

func installGit(instanceInfo InstanceInfo) {
	logrus.Infoln("installing git")
	switch instanceInfo.osType {
	case windowsString:
		ensureChocolatey()
		cmd := exec.Command("choco", "install", "git.install", "-y")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		gitErr := cmd.Run()
		if gitErr != nil {
			logrus.Errorf("failed to install choco: %s", gitErr)
		}
	default:
		cmd := exec.Command("apt-get", "install", "git")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			logrus.Errorf("failed to install git: %s", err)
		}
	}
}

func installDocker(instanceInfo InstanceInfo) {
	logrus.Infoln("installing docker")
	switch instanceInfo.osType {
	case windowsString:
		ensureChocolatey()
		cmd := exec.Command("choco", "install", "docker", "-y")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		gitErr := cmd.Run()
		if gitErr != nil {
			logrus.Errorf("failed to install docker: %s", gitErr)
			return
		}
	default:
		cmd := exec.Command("curl", "-fsSL", "https://get.docker.com", "-o", "get-docker.sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		getScriptErr := cmd.Run()
		if getScriptErr != nil {
			logrus.
				WithField("error", getScriptErr).
				Error("get docker install script failed")
			return
		}

		cmd = exec.Command("sudo", "sh", "get-docker.sh")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		dockerInstallErr := cmd.Run()
		if dockerInstallErr != nil {
			logrus.
				WithField("error", dockerInstallErr).
				Error("get docker install script failed")
			return
		}
	}
	logrus.Infoln("docker installed")
}
