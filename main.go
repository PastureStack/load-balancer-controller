package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/PastureStack/load-balancer-controller/controller"
	"github.com/PastureStack/load-balancer-controller/internal/logserver"
	"github.com/PastureStack/load-balancer-controller/provider"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var (
	VERSION = "dev"

	lbControllerName string
	lbProviderName   string
	metadataAddress  string

	lbc controller.LBController
	lbp provider.LBProvider
)

func main() {
	if err := logserver.Start(); err != nil {
		log.Fatalf("Unable to start log-level server: %v", err)
	}
	if os.Getenv("PASTURESTACK_DEBUG") == "true" || os.Getenv("RANCHER_DEBUG") == "true" {
		log.SetLevel(log.DebugLevel)
	}

	command := &cli.Command{
		Name:    "lb-controller",
		Usage:   "PastureStack HAProxy load-balancer service",
		Version: VERSION,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "controller", Value: "rancher", Usage: "Controller plugin name"},
			&cli.StringFlag{Name: "provider", Value: "haproxy", Usage: "Provider plugin name"},
			&cli.StringFlag{
				Name:    "metadata-address",
				Sources: cli.EnvVars("PASTURESTACK_METADATA_ADDRESS", "RANCHER_METADATA_ADDRESS"),
				Value:   "169.254.169.250",
				Usage:   "PastureStack metadata compatibility address",
			},
		},
		Action: run,
	}
	if err := command.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(_ context.Context, command *cli.Command) error {
	log.Infof("Starting PastureStack load-balancer service")
	lbControllerName = command.String("controller")
	lbProviderName = command.String("provider")
	metadataAddress = command.String("metadata-address")
	lbc = controller.GetController(lbControllerName, fmt.Sprintf("http://%s/2016-07-29", metadataAddress))
	if lbc == nil {
		return fmt.Errorf("unable to find controller by name %s", lbControllerName)
	}
	lbp = provider.GetProvider(lbProviderName)
	if lbp == nil {
		return fmt.Errorf("unable to find provider by name %s", lbProviderName)
	}
	log.Infof("LB controller: %s", lbc.GetName())
	log.Infof("LB provider: %s", lbp.GetName())

	go handleSigterm(lbc, lbp)
	go startHealthcheck()
	lbc.Run(lbp)
	return nil
}

func handleSigterm(controllerInstance controller.LBController, _ provider.LBProvider) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	log.Infof("Received SIGTERM, shutting down")

	exitCode := 0
	if err := controllerInstance.Stop(); err != nil {
		log.Infof("Error during shutdown %v", err)
		exitCode = 1
	}
	log.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}
