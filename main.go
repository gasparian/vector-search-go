package main

import (
	"net/http"
	"vector-search-go/app"
	cm "vector-search-go/common"
)

func main() {
	logger := cm.GetNewLogger()
	config, err := app.ParseEnv()
	if err != nil {
		logger.Err.Fatal(err.Error())
	}
	annServer, err := app.NewANNServer(logger, config)
	if err != nil {
		logger.Err.Fatal(err.Error())
	}
	defer annServer.Mongo.Disconnect()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.HealthCheck)
	mux.HandleFunc("/build-index", annServer.BuildHasherHandler)
	mux.HandleFunc("/check-build", annServer.CheckBuildHandler)
	mux.HandleFunc("/get-nn", annServer.GetNeighborsHandler)
	mux.HandleFunc("/pop-hash", annServer.PopHashRecordHandler)
	mux.HandleFunc("/put-hash", annServer.PutHashRecordHandler)
	http.Handle("/", cm.Decorate(mux, cm.Timer(logger)))
	logger.Err.Fatal(http.ListenAndServe(":8080", nil))
}
