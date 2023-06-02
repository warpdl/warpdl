package main

import (
	"errors"
	"os/exec"
)

func mux(a, v, o string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("please install ffmpeg in this system")
	}
	return exec.Command(
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-i", v,
		"-i", a,
		"-c:v", "copy",
		"-map", "0:v",
		"-map", "1:a",
		"-y", o,
	).Run()
}
