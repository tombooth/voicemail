package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/tombooth/masterslave"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

var (
	port          = flag.Int("port", 8080, "Port to open www server on")
	message       = flag.String("message", "Please leave a message", "Message to be said to the caller before recording")
	startEndpoint = flag.String("start", "/start", "Path of start endpoint")
	doneEndpoint  = flag.String("done", "/done", "Path of done endpoint")
	host          = flag.String("host", "REQUIRED", "Host of the service e.g. http://foo.com")
	authToken     = flag.String("authToken", "REQUIRED", "Used to verify the requests from twilio")
	amqpURI       = flag.String("amqpURI", "amqp://guest:guest@localhost:5672/", "AMQP URI")
)

func sortKeys(in url.Values) []string {
	keys := make([]string, len(in))

	i := 0
	for key, _ := range in {
		keys[i] = key
		i++
	}

	sort.Strings(keys)
	return keys
}

func validRequest(r *http.Request) bool {
	signature := r.Header.Get("X-Twilio-Signature")

	if len(signature) == 0 {
		return false
	}

	mac := hmac.New(sha1.New, []byte(*authToken))
	mac.Write([]byte(*host))
	mac.Write([]byte(r.RequestURI))

	if r.Method == "POST" {
		r.ParseForm()
		sortedKeys := sortKeys(r.PostForm)

		for _, key := range sortedKeys {
			value := strings.Join(r.PostForm[key], "")
			mac.Write([]byte(key))
			mac.Write([]byte(value))
		}
	}

	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return signature == expected
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" && validRequest(r) {
		w.Header().Add("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
		<Response>
			<Say>%s</Say>
			<Record maxLength="30" action="%s%s" />
		</Response>
		`, *message, *host, *doneEndpoint)
	} else {
		fmt.Println("Invalid start request")
	}
}

type Recording struct {
	From string
	To string
	Url string
}

func recordingHandler(toSlaves chan []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && validRequest(r) {
			r.ParseForm()
			recording := Recording {
				From: strings.Join(r.Form["From"], ""),
				To: strings.Join(r.Form["To"], ""),
				Url: strings.Join(r.Form["RecordingUrl"], ""),
			}

			fmt.Printf("%s: %s\n", recording.From, recording.Url)

			bytes, err := json.Marshal(recording)

			if err != nil {
				fmt.Println("Failed to marshall recording to json")
			} else {
				toSlaves <- bytes
			}
		} else {
			fmt.Println("Invalid recording done request")
		}
	}
}

func main() {

	flag.Parse()

	if *host == "REQUIRED" {
		fmt.Println("You need to define a host")
		os.Exit(1)
	}

	if *authToken == "REQUIRED" {
		fmt.Println("You need to define an auth token")
		os.Exit(1)
	}

	toSlaves, err := masterslave.Master(*amqpURI, "voicemail")

	if err != nil {
		fmt.Println("Master failed to startup:", err)
	}

	http.HandleFunc(*startEndpoint, startHandler)
	http.HandleFunc(*doneEndpoint, recordingHandler(toSlaves))

	fmt.Printf("Started www server on %d, listening...\n", *port)

	http.ListenAndServe(":" + strconv.Itoa(*port), nil)

}
