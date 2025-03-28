package utils

import (
	"io"
	"net/http"
	"strings"
)

// GetExternalIP retrieves the external IP address of the machine running the program.
// It makes a GET request to https://api64.ipify.org?format=text to fetch the IP address.
//
// The function returns two values:
// - A string representing the external IP address.
// - An error if any occurred during the HTTP request or reading the response body.
//
// Example:
//
//	ip, err := GetExternalIP()
//	if err != nil {
//		fmt.Println("Error fetching external IP:", err)
//		return
//	}
//	fmt.Println("External IP:", ip)
func GetExternalIP() (string, error) {
	resp, err := http.Get("https://api64.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(ip)), nil
}
