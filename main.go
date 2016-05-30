package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var (
	file        string = "samples.csv"
	concurrency int
	tick        string

	pool               chan bool
	currentRoutineSize uint64 = 0
	curRSize           uint64 = 0

	respOk  uint64 = 0
	respErr uint64 = 0

	responseTimes []int64
	client        *http.Client
)

func main() {

	flag.StringVar(&tick, "p", "1s", "log period")
	flag.IntVar(&concurrency, "c", 10, "concurrency")
	flag.StringVar(&file, "f", "samples.csv", "csv source")
	flag.Parse()

	tickDuration, err := time.ParseDuration(tick)
	logTimer := time.Tick(tickDuration)
	pool = make(chan bool, concurrency)
	responseTimes = []int64{}

	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println("GATTACK!GATTACK!")
	fmt.Println("print log every ", tickDuration)

	client = &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: concurrency}}
	f, err := os.Open(file)
	if err != nil {
		log.Fatalln(err)
	}

	go attack(f)
	for {

		select {
		case <-logTimer:

			curRSize = atomic.LoadUint64(&currentRoutineSize)

			curOk := atomic.LoadUint64(&respOk)
			curErr := atomic.LoadUint64(&respErr)
			totalReq := curOk + curErr
			throughP := float32(totalReq) / float32(tickDuration.Seconds())
			curResponseTimes := responseTimes

			log.Printf("\n~~~\n")
			fmt.Printf("ok - %d\n", curOk)
			fmt.Printf("errors - %d\n", curErr)
			fmt.Printf("total - %d\n", totalReq)
			fmt.Printf("t/s - %f\n", throughP)
			fmt.Printf("active/total - %d/%d\n", curRSize, concurrency)
			fmt.Printf("timings - %+v\n", curResponseTimes)

			fmt.Printf("~~~\n")

			atomic.SwapUint64(&respOk, uint64(0))
			atomic.SwapUint64(&respErr, uint64(0))
			responseTimes = []int64{}
		}
	}
}

func attack(f *os.File) (err error) {

	var (
		reader    *bufio.Reader
		buffer    []byte
		work      *Work
		recordLen int
	)

	reader = bufio.NewReader(f)

	defer func() {
		f.Close()
	}()

	for {
		buffer, _, err = reader.ReadLine()

		if err == io.EOF {
			_, err = f.Seek(0, 0)
		} else if err != nil {
			log.Fatalln(err)
			return err
		}

		if len(buffer) == 0 {
			continue
		}
		r := csv.NewReader(bytes.NewReader(buffer))
		record, err := r.Read()
		recordLen = len(record)

		if err == nil {
			work = &Work{}
			work.Url = record[0]

			if recordLen < 4 {
				err = errors.New("incorrect line format")
				return err
			}

			work.UserAgent = record[1]
			work.Method = record[2]
			work.Body = record[3]

			pool <- true
			go attackattack(work)

		}
	}
	return err
}

func attackattack(work *Work) {

	var (
		req      *http.Request
		err      error
		resp     *http.Response
		start    int64
		stop     int64
		duration int64
	)

	atomic.AddUint64(&currentRoutineSize, uint64(1))

	defer func() {
		atomic.AddUint64(&currentRoutineSize, ^uint64(0))
	}()

	req, err = prepareReq(work)

	start = time.Now().UnixNano()
	if err == nil {
		resp, err = client.Do(req)
	}

	if err == nil {
		defer func() {
			io.Copy(ioutil.Discard, resp.Body)
			resp.Body.Close()
		}()

		if resp.StatusCode < 400 {
			atomic.AddUint64(&respOk, uint64(1))
		} else {
			atomic.AddUint64(&respErr, uint64(1))
		}
	} else {
		atomic.AddUint64(&respErr, uint64(1))
	}
	stop = time.Now().UnixNano()

	duration = stop - start

	responseTimes = append(responseTimes, duration)
	<-pool
}

func prepareReq(work *Work) (req *http.Request, err error) {

	var (
		urlValues url.Values
	)

	if err != nil {
		return nil, err
	}

	req, err = http.NewRequest(strings.ToUpper(work.Method), work.Url, nil)
	req.Header.Add("User-Agent", work.UserAgent)
	if req.Method == http.MethodPost {
		urlValues, err = url.ParseQuery(work.Body)
		req.Form = urlValues
	}

	return req, err
}

type Work struct {
	Url       string
	UserAgent string
	Method    string
	Body      string
}