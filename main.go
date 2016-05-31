package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	//_ "net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"
)

var (
	file        string = "samples.csv"
	concurrency int
	tick        string
	verbose     bool
	profile     bool

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
	flag.BoolVar(&verbose, "v", false, "verbose mode")
	flag.BoolVar(&profile, "profile", false, "profile mode")
	flag.Parse()

	if profile {
		f_cpu_profiling, err := os.Create("profile.prof")
		//go func(){
		//	log.Println(http.ListenAndServe(":4000", nil))
		//}()
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f_cpu_profiling)
		defer func() {
			pprof.StopCPUProfile()
		}()

	}

	m := &runtime.MemStats{}
	tickDuration, err := time.ParseDuration(tick)
	logTimer := time.Tick(tickDuration)
	cProfile := time.Tick(30 * time.Second)
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

			if profile {
				runtime.ReadMemStats(m)
				log.Printf("~ Goroutines count %d\n", runtime.NumGoroutine())
				log.Printf("~ Memory HeapAlloc %d\n", m.HeapAlloc/1024)
				log.Printf("~ Memory HeapInuse %d\n", m.HeapInuse/1024)

				log.Printf("~ Memory Alloc %d\n", m.Alloc)
				log.Printf("~ Memory Mallocs %d\n", m.Mallocs)
				log.Printf("~ Memory Frees %d\n", m.Frees)
			}

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
			fmt.Printf("active/total #1 - %d/%d\n", curRSize, concurrency)
			fmt.Printf("active/total #2 - %d/%d\n", len(pool), concurrency)
			fmt.Printf("timings - %+v\n", curResponseTimes)

			fmt.Printf("~~~\n")

			atomic.SwapUint64(&respOk, uint64(0))
			atomic.SwapUint64(&respErr, uint64(0))
			responseTimes = []int64{}
		case <-cProfile:
			if profile {
				var f_heap_profiling io.Writer
				f_heap_profiling, err = os.Create("profile_heap.prof")
				pprof.WriteHeapProfile(f_heap_profiling)
				fmt.Printf("profile_heap done")

			}
		}
	}
}

func attack(f *os.File) {

	var (
		err       error
		reader    *bufio.Reader
		buffer    []byte
		b         []byte
		work      *Work
		recordLen int
		record    []string
		isPrefix  bool
	)

	reader = bufio.NewReader(f)

	defer func() {
		f.Close()
	}()

	buffer = []byte{}
	for {
		b, isPrefix, err = reader.ReadLine()

		buffer = append(buffer, b...)

		if isPrefix {
			continue
		}

		if err == io.EOF {
			if verbose {
				log.Println("io.EOF")
			}
			_, err = f.Seek(0, 0)
		} else if err != nil {
			if verbose {
				log.Println("err", err)
			}
			log.Fatalln(err)
		}

		if verbose {
			log.Println("readline", string(buffer))
		}
		r := csv.NewReader(bytes.NewReader(buffer))
		record, err = r.Read()
		recordLen = len(record)

		if verbose {
			log.Println("csv record ", recordLen, record)
		}

		if err == nil {
			work = &Work{}
			work.Url = record[0]

			if recordLen < 4 {
				log.Fatalln("incorrect line format")
			}

			work.UserAgent = record[1]
			work.Method = record[2]
			work.Body = record[3]

			pool <- true
			go attackattack(work)
		} else {
			log.Fatalln(err)
		}
	}
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

	//if verbose {
	//	log.Println("request", req, err)
	//}

	start = time.Now().UnixNano()

	if err == nil {
		resp, err = client.Do(req)
	}

	//if verbose {
	//	log.Println("response", resp, err)
	//}

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
