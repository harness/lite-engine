package setup

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/sirupsen/logrus"
)

const windowsString = "windows"

type InstanceInfo struct {
	osType string
}

func GetInstanceInfo() InstanceInfo {
	osType := runtime.GOOS
	return InstanceInfo{osType: osType}
}

func PrepareService() (err error) {
	instanceInfo := GetInstanceInfo()
	if instanceInfo.osType == windowsString {
		ensureChocolatey(instanceInfo)
		if !NSSMInstalled(instanceInfo) {
			installNSSM(instanceInfo)
		}
		err = startLiteEngineService(instanceInfo)
	}
	return err
}

func PrepareSystem() {
	instanceInfo := GetInstanceInfo()
	NSSMInstalled(instanceInfo)
	if !GitInstalled(instanceInfo) {
		installGit(instanceInfo)
	}
	if !DockerInstalled(instanceInfo) {
		installDocker(instanceInfo)
	}
}

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

func NSSMInstalled(instanceInfo InstanceInfo) (installed bool) {
	if instanceInfo.osType == windowsString {
		logrus.Infoln("checking nssm is installed")
		cmd := exec.Command("nssm", "--version")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		nssmErr := cmd.Run()
		if nssmErr != nil {
			logrus.Infoln("nssm is not installed")
			return false
		}
		return true
	}
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

func ensureChocolatey(instanceInfo InstanceInfo) {
	if instanceInfo.osType == windowsString {
		cmd := exec.Command("choco", "-h")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			logrus.Infoln("installing chocolatey")
			cmd := exec.Command("PowerShell.exe", "-command", "Set-ExecutionPolicy", "Bypass", "-Scope", "Process", "-Force;", "[System.Net.ServicePointManager]::SecurityProtocol", "=",
				"[System.Net.ServicePointManager]::SecurityProtocol", "-bor", "3072;", "iex", "((New-Object System.Net.WebClient).DownloadString('https://chocolatey.org/install.ps1'))")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			chocoErr := cmd.Run()
			if chocoErr != nil {
				logrus.Errorf("failed to install chocolatey: %s", chocoErr)
			}
			// path does not have chocolatey on it until we have a new powershell session.
		}
		logrus.Infoln("chocolatey installed")
	}
}

func installGit(instanceInfo InstanceInfo) {
	logrus.Infoln("installing git")
	switch instanceInfo.osType {
	case windowsString:
		ensureChocolatey(instanceInfo)
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

func installNSSM(instanceInfo InstanceInfo) {
	if instanceInfo.osType == windowsString {
		logrus.Infoln("installing nssm")
		// path does not have chocolatey on it until we have a new powershell session.
		cmd := exec.Command(`C:\ProgramData\chocolatey\bin\choco.exe`, "install", "nssm", "-r", "-y")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		nssmErr := cmd.Run()
		if nssmErr != nil {
			logrus.Errorf("failed to install nssm: %s", nssmErr)
			return
		}
		logrus.Infoln("installed nssm")
	}
}

func startLiteEngineService(instanceInfo InstanceInfo) (err error) {
	if instanceInfo.osType == windowsString {
		logrus.Infoln("starting lite-engine as a service")
		// path does not have chocolatey on it until we have a new powershell session.
		cmd := exec.Command(`C:\ProgramData\chocolatey\lib\NSSM\tools\nssm.exe`, "install", "lite-engine", `"""""""C:\Program Files\lite-engine\lite-engine.exe"""""""`,
			"server", `--env-file="""""""C:\Program Files\lite-engine\.env"""""""`)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		addErr := cmd.Run()
		if addErr != nil {
			addErr = fmt.Errorf("failed to add lite-engine as a service: %s", addErr)
			logrus.Error(addErr)
			return addErr
		}
		cmd = exec.Command(`C:\ProgramData\chocolatey\lib\NSSM\tools\nssm.exe`, "start", "lite-engine")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		startErr := cmd.Run()
		if startErr != nil {
			startErr = fmt.Errorf("failed to start lite-engine as a service: %s", startErr)
			logrus.Error(startErr)
			return startErr
		}
		logrus.Infoln("starting lite-engine started")
	}
	return err
}

func installDocker(instanceInfo InstanceInfo) {
	logrus.Infoln("installing docker")
	switch instanceInfo.osType {
	case windowsString:
		ensureChocolatey(instanceInfo)
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
