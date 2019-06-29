package main

import (
	"io/ioutil"
	stdlog "log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
)

var (
	app       string
	version   string
	branch    string
	revision  string
	buildDate string
	goVersion = runtime.Version()
)

var (
	// flags
	mtu                   = kingpin.Flag("mtu", "The network mtu").Default("1500").OverrideDefaultFromEnvar("MTU").String()
	registryMirror        = kingpin.Flag("registry-mirror", "An optional registry mirror address").Envar("MIRROR").String()
	containerListFilePath = kingpin.Flag("container-list-file-path", "Path to the yaml file with a list of containers to preheat").Default("/configs/container-list.yaml").OverrideDefaultFromEnvar("CONTAINER_LIST_FILE_PATH").String()

	// seed random number
	r = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func main() {

	// parse command line parameters
	kingpin.Parse()

	// log as severity for stackdriver logging to recognize the level
	zerolog.LevelFieldName = "severity"

	// set some default fields added to all logs
	log.Logger = zerolog.New(os.Stdout).With().
		Timestamp().
		Str("app", app).
		Str("version", version).
		Logger()

	// use zerolog for any logs sent via standard log library
	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)

	// log startup message
	log.Info().
		Str("branch", branch).
		Str("revision", revision).
		Str("buildDate", buildDate).
		Str("goVersion", goVersion).
		Str("mtu", *mtu).
		Str("registryMirror", *registryMirror).
		Msgf("Starting %v version %v...", app, version)

	// define channel used to gracefully shutdown the application
	gracefulShutdown := make(chan os.Signal)
	signal.Notify(gracefulShutdown, syscall.SIGTERM, syscall.SIGINT)

	dockerRunner := NewDockerRunner(*mtu, *registryMirror)

	err := dockerRunner.startDockerDaemon()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed starting docker daemon")
	}

	dockerRunner.waitForDockerDaemon()

	// wait for mirror to be ready
	for {
		err := dockerRunner.runDockerPull("alpine")
		if err == nil {
			break
		}
		sleepWithJitter(10)
	}

	go func() {
		// loop indefinitely
		for {
			// get list of containers to preheat
			log.Info().Msgf("Reading %v file...", *containerListFilePath)

			data, err := ioutil.ReadFile(*containerListFilePath)
			if err != nil {
				log.Warn().Err(err).Msgf("Failed reading file %v", *containerListFilePath)
				sleepWithJitter(900)
				continue
			}

			// unmarshal strict, so non-defined properties or incorrect nesting will fail
			var containerList ContainerList
			if err := yaml.UnmarshalStrict(data, &containerList); err != nil {
				log.Warn().Err(err).Msgf("Failed unmarshaling file %v", *containerListFilePath)
				sleepWithJitter(900)
				continue
			}

			var wg sync.WaitGroup
			wg.Add(len(containerList.Containers))

			// pull all images in parallel
			for _, c := range containerList.Containers {
				go func(container string) {
					defer wg.Done()
					dockerRunner.runDockerPull(container)
					dockerRunner.runDockerRemoveImage(container)
				}(c)
			}

			// wait for all pulls to finish
			wg.Wait()

			sleepWithJitter(900)
		}
	}()

	// block until SIGTERM
	<-gracefulShutdown
	log.Info().Msg("Shutting down...")
}

func sleepWithJitter(input int) {
	sleepTime := applyJitter(input)
	log.Info().Msgf("Sleeping for %v seconds...", sleepTime)
	time.Sleep(time.Duration(sleepTime) * time.Second)
}

func applyJitter(input int) (output int) {

	deviation := int(0.25 * float64(input))

	return input - deviation + r.Intn(2*deviation)
}

func runCommandExtended(command string, args []string) error {
	log.Printf("Running command '%v %v'...", command, strings.Join(args, " "))
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return err
}
