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

func GetCredentials(path string) (*Credentials, error) {
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

func GetDefaultConfigPath() (*string, error) {
	path := os.Getenv("OANDA_CREDENTIALS_PATH")
	if path != "" {
		return &path, nil
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		path := fmt.Sprintf("%s/.config/oanda/credentials.yaml", home)
		return &path, nil
	}
}

func main() {
	defaultConfig, err := GetDefaultConfigPath()
	if err != nil {
		log.Fatal(err)
	}

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
						Name:    "heartbeat-timeout",
						Aliases: []string{"t"},
						Value:   6 * time.Second,
					},
					&cli.BoolFlag{
						Name:    "all-instruments",
						Aliases: []string{"a"},
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Value:   *defaultConfig,
					},
				},
			},
			{
				Name:    "candles",
				Aliases: []string{"p"},
				Usage:   "Get candles stream by polling",
				Action:  candlesAction,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "instrument",
						Aliases:  []string{"i"},
						Required: true,
					},
					&cli.StringFlag{
						Name:    "granularity",
						Aliases: []string{"g"},
						Value:   "S5",
					},
					&cli.TimestampFlag{
						Name:        "from",
						Layout:      time.RFC3339,
						DefaultText: time.Now().Format(time.RFC3339),
					},
					&cli.DurationFlag{
						Name:    "polling-interval",
						Aliases: []string{"p"},
						Value:   1 * time.Second,
					},
					&cli.BoolFlag{
						Name: "completed-only",
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Value:   *defaultConfig,
					},
				},
			},
			{
				Name:    "transactions",
				Aliases: []string{"t"},
				Usage:   "Get transaction stream",
				Action:  transactionsAction,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name: "heartbeat",
					},
					&cli.DurationFlag{
						Name:    "heartbeat-timeout",
						Aliases: []string{"t"},
						Value:   5 * time.Second,
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Value:   *defaultConfig,
					},
				},
			},
		},
	}

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func pricingAction(c *cli.Context) error {
	instruments := c.String("instruments")
	heartbeat := c.Bool("heartbeat")
	heartbeatTimeout := c.Duration("heartbeat-timeout")
	configPath := c.String("config")
	err := getStream(instruments, heartbeat, heartbeatTimeout, configPath)

	return err
}

func getStream(instruments string, heartbeat bool, heartbeatTimeout time.Duration, configPath string) error {
	credentials, err := GetCredentials(configPath)
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

func candlesAction(c *cli.Context) error {
	instrument := c.String("instrument")
	granularity := c.String("granularity")

	from := time.Now()
	_from := c.Timestamp("from")
	if _from != nil {
		from = *_from
	}

	pollingInterval := c.Duration("polling-interval")
	completedOnly := c.Bool("completed-only")
	configPath := c.String("config")
	err := getCandlesStream(instrument, granularity, from, pollingInterval, completedOnly, configPath)

	return err
}

func getCandlesStream(instrument string, granularity string, from time.Time, pollingInterval time.Duration, completedOnly bool, configPath string) error {
	credentials, err := GetCredentials(configPath)
	if err != nil {
		return err
	}

	var lastCandle *Candlestick = nil

	for {
		candles, err := getCandlesForStream(credentials, instrument, granularity, from)
		if err != nil {
			return err
		}

		for _, candle := range *candles {
			if completedOnly && candle.Complete == false {
				continue
			}

			if lastCandle == nil || candle.NewerThan(lastCandle) {
				bytes, err := json.Marshal(candle)
				if err != nil {
					return err
				}
				fmt.Println(string(bytes))
			}
		}

		if len(*candles) != 0 {
			lastCandle = &(*candles)[len(*candles)-1]
			from = lastCandle.Time
		}

		time.Sleep(pollingInterval)
	}
}

func GetIntPointer(val int) *int {
	return &val
}

type CandlesResponseBody struct {
	Candles     *[]Candlestick `json:"candles"`
	Granularity string         `json:"granularity"`
	Instrument  string         `json:"instrument"`
}

type Candlestick struct {
	Complete bool             `json:"complete"`
	Volume   int              `json:"volume"`
	Time     time.Time        `json:"time"`
	Mid      *CandlestickData `json:"mid"`
	Bid      *CandlestickData `json:"bid"`
	Ask      *CandlestickData `json:"ask"`
}

func (self *Candlestick) NewerThan(other *Candlestick) bool {
	if self.Time.After(other.Time) {
		return true
	} else if self.Time.Equal(other.Time) {
		if self.Complete == false && other.Complete == false {
			return self.Volume > other.Volume
		} else if self.Complete == true && other.Complete == false {
			return true
		} else {
			return false
		}
	} else {
		return false
	}
}

type CandlestickData struct {
	O string `json:"o"`
	H string `json:"h"`
	L string `json:"l"`
	C string `json:"c"`
}

func getCandlesForStream(credentials *Credentials, instrument string, granularity string, from time.Time) (*[]Candlestick, error) {
	account := credentials.Default

	baseUrl := "https://api-fxpractice.oanda.com"
	query := fmt.Sprintf("from=%s&granularity=%s&price=MBA&count=5000", from.Format(time.RFC3339), granularity)
	url := fmt.Sprintf("%s/v3/instruments/%s/candles?%s", baseUrl, instrument, query)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", account.Token))

	client := new(http.Client)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != 200 {
		fmt.Fprintln(os.Stderr, res.Status)
		return nil, errors.New(string(bytes))
	}

	// fmt.Fprintln(os.Stderr, string(bytes))

	var body CandlesResponseBody
	if err := json.Unmarshal(bytes, &body); err != nil {
		return nil, err
	}

	return body.Candles, nil
}

func transactionsAction(c *cli.Context) error {
	heartbeat := c.Bool("heartbeat")
	heartbeatTimeout := c.Duration("heartbeat-timeout")
	configPath := c.String("config")
	err := getTransactionStream(heartbeat, heartbeatTimeout, configPath)

	return err
}

func getTransactionStream(heartbeat bool, heartbeatTimeout time.Duration, configPath string) error {
	credentials, err := GetCredentials(configPath)
	if err != nil {
		return err
	}
	account := credentials.Default

	baseUrl := "https://stream-fxpractice.oanda.com"
	url := fmt.Sprintf("%s/v3/accounts/%s/transactions/stream", baseUrl, account.AccountId)

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

		var th TransactionOrHeartbeat
		if err := json.Unmarshal(line, &th); err != nil {
			return err
		}

		if th.Type == "HEARTBEAT" {
			if heartbeatTimeout != 0 {
				heartbeatChannel <- struct{}{}
			}
			if heartbeat {
				fmt.Println(string(line))
			}
		} else {
			fmt.Println(string(line))
		}
	}

	return err
}

type TransactionOrHeartbeat struct {
	Type string `json:"type"`
}
