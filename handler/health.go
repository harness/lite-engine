package handler

import (
	"net/http"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/setup"
	"github.com/sirupsen/logrus"
)

func HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logrus.Infoln("handler: HandleHealth()")
		instanceInfo := setup.GetInstanceInfo()
		dockerOK := setup.DockerInstalled(instanceInfo)
		gitOK := setup.GitInstalled(instanceInfo)
		response := api.HealthResponse{
			DockerInstalled: dockerOK,
			GitInstalled:    gitOK,
			LiteEngineLog:   setup.GetLiteEngineLog(instanceInfo),
			OK:              dockerOK && gitOK,
		}
		WriteJSON(w, response, http.StatusOK)
	}
}
