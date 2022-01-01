package service

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alexzorin/authy"
	"github.com/zalando/go-keyring"
)

// DeviceRegistration device register info
type DeviceRegistration struct {
	UserID       uint64 `json:"user_id,omitempty"`
	DeviceID     uint64 `json:"device_id,omitempty"`
	Seed         string `json:"seed,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	MainPassword string `json:"main_password,omitempty"`
}

// NewDeviceConfig new device config
type NewDeviceConfig struct {
	CountryCode string
	Mobile      string
	Password    string
}

// Device ..
type Device struct {
	conf         NewDeviceConfig
	registration DeviceRegistration
	tokenMap     map[string]*Token
	tokens       []*Token
}

// NewDevice ..
func NewDevice(conf NewDeviceConfig) *Device {
	d := &Device{
		conf: conf,
	}

	d.RegisterOrGetDeviceInfo()

	return d
}

// RegisterOrGetDeviceInfo get device info from local cache, if not exist register a new device
func (d *Device) RegisterOrGetDeviceInfo() (devInfo DeviceRegistration) {
	devInfo, err := d.LoadExistingDeviceInfo()
	if err == nil && devInfo.UserID != 0 {
		d.registration = devInfo
		return
	}

	err = os.ErrNotExist

	if os.IsNotExist(err) {
		devInfo, err = d.newRegistrationDevice()
		if err != nil {
			os.Exit(1)
		}

		log.Println("Register device successfully!!!")
		log.Printf("Your device id: %v\n", devInfo.DeviceID)
		os.Exit(0)
	}

	if err != nil {
		log.Println("Load/Register device info failed", err)
	}

	return
}

func getCountryCodeAndMobile(countrycode, mobile string) (int, string, error) {
	var (
		sc      = bufio.NewScanner(os.Stdin)
		codeInt int
	)

	if len(countrycode) == 0 {
		fmt.Print("\nWhat is your phone number's country code? (digits only, e.g. 86): ")
		if !sc.Scan() {
			err := errors.New("Please provide a phone country code, e.g. 86")
			log.Println(err)
			return 0, "", err
		}

		countrycode = sc.Text()
	}

	codeInt, err := strconv.Atoi(strings.TrimSpace(countrycode))
	if err != nil {
		log.Println("Invalid country code. Parse country code failed", err)
		return 0, "", err
	}

	if len(mobile) == 0 {
		fmt.Print("\nWhat is your phone number? (digits only): ")
		if !sc.Scan() {
			err = errors.New("Please provide a phone number, e.g. 1232211")
			log.Println(err)
			return 0, "", err
		}

		mobile = sc.Text()
	}

	mobile = strings.TrimSpace(mobile)

	return codeInt, mobile, nil
}

func registerDevice(client authy.Client, userStatus authy.UserStatus) (resp authy.CompleteDeviceRegistrationResponse, err error) {
	// Begin a device registration using Authy app push notification
	regStart, err := client.RequestDeviceRegistration(nil, userStatus.AuthyID, authy.ViaMethodPush)
	if err != nil {
		log.Println("Start register device failed", err)
		return
	}

	if !regStart.Success {
		err = fmt.Errorf("Authy did not accept the device registration request: %+v", regStart)
		log.Println(err)
		return
	}

	var regPIN string
	timeout := time.Now().Add(5 * time.Minute)
	for {
		if timeout.Before(time.Now()) {
			err = errors.New("Gave up waiting for user to respond to Authy device registration request")
			log.Println(err)
			return
		}

		log.Printf("Checking device registration status (%s until we give up)", time.Until(timeout).Truncate(time.Second))

		regStatus, err1 := client.CheckDeviceRegistration(nil, userStatus.AuthyID, regStart.RequestID)
		if err1 != nil {
			err = err1
			log.Println(err)
			return
		}
		if regStatus.Status == "accepted" {
			regPIN = regStatus.PIN
			break
		} else if regStatus.Status != "pending" {
			err = fmt.Errorf("Invalid status while waiting for device registration: %s", regStatus.Status)
			log.Println(err)
			return
		}

		time.Sleep(5 * time.Second)
	}

	resp, err = client.CompleteDeviceRegistration(nil, userStatus.AuthyID, regPIN)
	if err != nil {
		log.Println(err)
		return
	}

	if resp.Device.SecretSeed == "" {
		err = errors.New("Something went wrong completing the device registration")
		log.Println(err)
		return
	}

	return
}

func (d *Device) newRegistrationDevice() (devInfo DeviceRegistration, err error) {

	codeInt, mobile, err := getCountryCodeAndMobile(d.conf.CountryCode, d.conf.Mobile)

	client, err := authy.NewClient()
	if err != nil {
		log.Println("New authy client failed", err)
		return
	}

	userStatus, err := client.QueryUser(nil, codeInt, mobile)
	if err != nil {
		log.Println("Query user failed", err)
		return
	}

	if !userStatus.IsActiveUser() {
		err = errors.New("There doesn't seem to be an Authy account attached to that phone number")
		log.Println(err)
		return
	}

	resp, err := registerDevice(client, userStatus)

	devInfo = DeviceRegistration{
		UserID:   resp.AuthyID,
		DeviceID: resp.Device.ID,
		Seed:     resp.Device.SecretSeed,
		APIKey:   resp.Device.APIKey,
	}

	d.registration = devInfo

	err = d.SaveDeviceInfo()
	if err != nil {
		log.Println("Save device info failed", err)
	}

	return
}

// SaveDeviceInfo ..
func (d *Device) SaveDeviceInfo() (err error) {
	res, err := json.Marshal(d.registration)
	if err != nil {
		return
	}

	err = keyring.Set("authy", d.conf.Mobile, string(res))
	if err != nil {
		return
	}
	return
}

// LoadExistingDeviceInfo ...
func (d *Device) LoadExistingDeviceInfo() (devInfo DeviceRegistration, err error) {
	res, err := keyring.Get("authy", d.conf.Mobile)
	if err != nil {
		return
	}

	err = json.Unmarshal([]byte(res), &devInfo)
	return
}

// DeleteMainPassword delete main password
func (d *Device) DeleteMainPassword() {
	d.registration.MainPassword = ""
	err := d.SaveDeviceInfo()
	if err != nil {
		log.Fatal("SaveDeviceInfo failed", err)
	}
}
