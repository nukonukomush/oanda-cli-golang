package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"
)

type Credentials struct {
	Default struct {
		AccountId string `yaml:"account_id"`
		Token     string `yaml:"token"`
	}
}

func GetCredentials() (*Credentials, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/.config/oanda/credentials.yaml", home)
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	credentials := Credentials{}
	err = yaml.Unmarshal(bytes, &credentials)
	if err != nil {
		return nil, err
	}

	return &credentials, nil
}

func main() {
	app := &cli.App{
		Name:  "oanda-cli",
		Usage: "oanda v20 cli",
		Commands: []*cli.Command{
			{
				Name:    "pricing",
				Aliases: []string{"p"},
				Usage:   "Get pricing stream",
				Action:  pricingAction,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "instruments",
						Aliases: []string{"i"},
						Usage:   "List of instruments (CSV)",
					},
					&cli.BoolFlag{
						Name: "heartbeat",
					},
					&cli.DurationFlag{
						Name:    "heartbeat-timeout-sec",
						Aliases: []string{"t"},
					},
					&cli.BoolFlag{
						Name:    "all-instruments",
						Aliases: []string{"a"},
					},
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func pricingAction(c *cli.Context) error {
	instruments := c.String("instruments")
	heartbeat := c.Bool("heartbeat")
	heartbeatTimeout := c.Duration("heartbeat-timeout-sec")
	err := getStream(instruments, heartbeat, heartbeatTimeout)

	return err
}

func getStream(instruments string, heartbeat bool, heartbeatTimeout time.Duration) error {
	credentials, err := GetCredentials()
	if err != nil {
		return err
	}
	account := credentials.Default

	baseUrl := "https://stream-fxpractice.oanda.com"
	query := fmt.Sprintf("instruments=%s", instruments)
	url := fmt.Sprintf("%s/v3/accounts/%s/pricing/stream?%s", baseUrl, account.AccountId, query)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", account.Token))

	client := new(http.Client)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		fmt.Fprintln(os.Stderr, res.Status)
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return err
		}
		return errors.New(string(body))
	}

	heartbeatChannel := make(chan struct{})

	if heartbeatTimeout != 0 {
		go func() {
			for {
				select {
				case <-heartbeatChannel:
				case <-time.After(heartbeatTimeout):
					panic("heartbeat timeout")
				}
			}
		}()
	}

	reader := bufio.NewReader(res.Body)
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			return err
		}
		if line == nil {
			break
		}

		var ph PriceOrHeartbeat
		if err := json.Unmarshal(line, &ph); err != nil {
			return err
		}

		if ph.Type == "PRICE" {
			fmt.Println(string(line))
		} else if ph.Type == "HEARTBEAT" {
			if heartbeatTimeout != 0 {
				heartbeatChannel <- struct{}{}
			}
			if heartbeat {
				fmt.Println(string(line))
			}
		}
	}

	return err
}

type PriceOrHeartbeat struct {
	Type string `json:"type"`
}
