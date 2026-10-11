#!/bin/sh
# Writes three synthetic test tracks into audio/, so that the project needs no audio files of its own, but only when audio/
# holds no audio file at all: a project with real songs in audio/ plays those and nothing else.
set -eu

cd "$(dirname "$0")/.."
mkdir -p audio

# The extensions that the server reads, in any case.
for file in audio/*; do
	case "$(printf '%s' "$file" | tr '[:upper:]' '[:lower:]')" in
	*.aac | *.flac | *.m4a | *.mp3 | *.ogg | *.opus | *.wav) exit 0 ;;
	esac
done

tone() {
	name=$1
	low=$2
	high=$3
	seconds=$4
	ffmpeg -v error -y \
		-f lavfi -t "$seconds" -i "sine=frequency=$low:sample_rate=48000" \
		-f lavfi -t "$seconds" -i "sine=frequency=$high:sample_rate=48000" \
		-filter_complex "amix=inputs=2" -ar 48000 -ac 2 "audio/$name"
}

tone 01-third.wav 220 277 30
tone 02-fifth.wav 330 495 25
tone 03-octave.wav 440 880 20
