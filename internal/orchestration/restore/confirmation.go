package restore

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// PromptForConfirmation prompts the user for confirmation and returns true if they confirm
func PromptForConfirmation() bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Do you want to continue? (yes/no): ")

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "yes" || response == "y"
}
