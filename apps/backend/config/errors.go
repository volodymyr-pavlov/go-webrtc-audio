package config

import "errors"

// ErrNoHTTPAddr is returned by [Options.Validate] when the address of the HTTP server is empty. Like the other errors
// that function joins, one for each rule a setting can break, it is usually wrapped with the value that broke the rule.
var ErrNoHTTPAddr = errors.New("the address of the HTTP server is required")

// ErrNoAudioDir is returned when the directory with the audio files is empty.
var ErrNoAudioDir = errors.New("the directory with the audio files is required")

// ErrICEPort is returned for a UDP port that is not from 1 to 65535.
var ErrICEPort = errors.New("the UDP port must be from 1 to 65535")

// ErrMode is returned for a mode that the engine does not know.
var ErrMode = errors.New("the mode must be naive, track or fanout")

// ErrNegative is wrapped after the name of a setting that must be 0 or more.
var ErrNegative = errors.New("must be 0 or more")

// ErrBatchDelay is returned for a batch delay that is as long as the frame or longer: the sender would hold every
// batch past the next packet.
var ErrBatchDelay = errors.New("the batch delay must be shorter than the frame")

// ErrTURNTTL is returned for a TURN TTL of 0 or less when a TURN server is set.
var ErrTURNTTL = errors.New("the TURN TTL must be above 0")

// ErrTURNNeedsSecret is returned when TURN addresses are set without the file with the secret.
var ErrTURNNeedsSecret = errors.New("a TURN address needs a TURN secret file: without a secret the credentials cannot work")

// ErrTURNNeedsURL is returned when the TURN secret file is set without TURN addresses.
var ErrTURNNeedsURL = errors.New("a TURN secret file needs a TURN address: without one no credentials are handed out")

// ErrPresenterMode is returned when the AI presenter is asked for in a mode that has no shared stream, because every
// listener of such a mode has a stream of its own.
var ErrPresenterMode = errors.New("the AI presenter needs the mode fanout or track: in naive mode every listener has a stream of its own")

// ErrMissingFromEnv is wrapped after the name of a mandatory field of [Env] that is nil.
var ErrMissingFromEnv = errors.New("missing from the environment")
