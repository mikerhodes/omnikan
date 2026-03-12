package omnifocus

import (
	"log"
	"os"
	"os/exec"
)

// executeScript runs jsCode via osascript, passing args as OSA_ARGS, and
// returns stdout. All scripts expect a JSON object in OSA_ARGS and write a
// JSON document to stdout.
func executeScript(jsCode []byte, args []byte) ([]byte, error) {
	cmd := exec.Command("/usr/bin/osascript", "-l", "JavaScript")

	cmd.Env = append(os.Environ(), "OSA_ARGS="+string(args))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	go func() {
		defer stdin.Close()
		if _, err := stdin.Write(jsCode); err != nil {
			log.Fatal(err)
		}
	}()

	return cmd.Output()
}
