package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// DockerRunner pulls and runs docker containers
type DockerRunner interface {
	startDockerDaemon() error
	waitForDockerDaemon()

	runDockerPull(containerImage string) error
	runDockerRemoveImage(containerImage string) error
	runDockerSystemPrune() error
}

type dockerRunnerImpl struct {
	mtu            string
	registryMirror string
}

// NewDockerRunner returns a new DockerRunner
func NewDockerRunner(mtu, registryMirror string) DockerRunner {
	return &dockerRunnerImpl{
		mtu:            mtu,
		registryMirror: registryMirror,
	}
}

func (dr *dockerRunnerImpl) startDockerDaemon() error {

	// dockerd --host=unix:///var/run/docker.sock --host=tcp://0.0.0.0:2375 --storage-driver=$STORAGE_DRIVER &
	log.Debug().Msg("Starting docker daemon...")
	args := []string{"--host=unix:///var/run/docker.sock", fmt.Sprintf("--mtu=%v", dr.mtu), "--host=tcp://0.0.0.0:2375", "--storage-driver=overlay2", "--max-concurrent-downloads=10"}

	// if a registry mirror is set in config configured docker daemon to use it
	if dr.registryMirror != "" {
		args = append(args, fmt.Sprintf("--registry-mirror=%v", dr.registryMirror))
	}

	log.Debug().Msgf("dockerd %v", strings.Join(args, " "))

	dockerDaemonCommand := exec.Command("dockerd", args...)
	dockerDaemonCommand.Stdout = log.Logger
	dockerDaemonCommand.Stderr = log.Logger
	err := dockerDaemonCommand.Start()
	if err != nil {
		return err
	}

	return nil
}

func (dr *dockerRunnerImpl) waitForDockerDaemon() {

	// wait until /var/run/docker.sock exists
	log.Debug().Msg("Waiting for docker daemon to be ready for use...")
	for {
		if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
			// does not exist
			time.Sleep(1000 * time.Millisecond)
		} else {
			// file exists, break out of for loop
			break
		}
	}
	log.Debug().Msg("Docker daemon is ready for use")
}

func (dr *dockerRunnerImpl) runDockerPull(containerImage string) (err error) {

	log.Info().Msgf("Pulling docker image '%v'", containerImage)

	pullArgs := []string{
		"pull",
		containerImage,
	}
	err = runCommandExtended("docker", pullArgs)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed pulling container image '%v'", containerImage)
	}

	return
}

func (dr *dockerRunnerImpl) runDockerRemoveImage(containerImage string) (err error) {

	log.Info().Msgf("Removing docker image '%v'", containerImage)

	pullArgs := []string{
		"rmi",
		containerImage,
	}
	err = runCommandExtended("docker", pullArgs)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed removing container image '%v'", containerImage)
	}

	return
}

func (dr *dockerRunnerImpl) runDockerSystemPrune() (err error) {

	log.Info().Msg("Pruning docker system")

	pullArgs := []string{
		"system",
		"prune",
		"--all",
		"--force",
	}
	err = runCommandExtended("docker", pullArgs)
	if err != nil {
		log.Warn().Err(err).Msg("Failed pruning system")
	}

	return
}
