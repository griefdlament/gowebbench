package main

import "fmt"
import "flag"
import "os"
import "net/http"
import "time"
import "runtime"
import "io/ioutil"
import "log"
import "regexp"
import "bytes"
import "io"
import "bufio"
import "strings"
import "strconv"
import "net/url"

type Response_Stat struct {
  status string
  response_time time.Duration
  amount_of_data int
}

var f *os.File

var client *http.Client

var transport *http.Transport

func main() {
  uriPtr := flag.String("uri", "", "[Input mode] target uri for testing (use only one input mode)")
  userPtr := flag.Int("user", 1, "number of concurrent user")
  transPtr := flag.Int("trans", 1, "number of transaction for user to do request (for single target url only)")
  filePtr := flag.String("output", "load.log", "path or filename for text output file")
  inputListPtr := flag.String("input", "", "[Input mode] path or filename for input file (use only one input mode)")
  flag.Parse()

  if *uriPtr == "" && *inputListPtr == "" {
    fmt.Println("Please specify target uri by using -uri=arg argument.")
    fmt.Println("Or specify input file path.")
    os.Exit(1)
  }

  if *uriPtr != "" && *inputListPtr != "" {
    fmt.Println("Both input mode specify.")
    fmt.Println("Use only one input mode. (-uri or -input flag)")
    os.Exit(1)
  }

  _, err := url.Parse(*uriPtr)
  if err != nil {
    log.Printf("%T %+v\n", err, err)
    os.Exit(1)
  }

  runtime.GOMAXPROCS(runtime.NumCPU())

  reader := bufio.NewReader(os.Stdin)
  fmt.Println("Execution might interrupt target server's function, Are you SURE?")
  fmt.Print("Confirm(y/n): ")
  in, _ := reader.ReadString('\n')
  in = strings.TrimSpace(in)
  if in == "y" || in == "Y" {
    fmt.Println("")
    load(*uriPtr, *userPtr, *transPtr, *inputListPtr, *filePtr)
  }
}

func load(uri string, user int, trans int, input string, filename string) {
  var err error
  if _, err = os.Stat(filename); err == nil {
    os.Remove(filename)
  }
  f, err = os.OpenFile(filename, os.O_WRONLY | os.O_CREATE, 0666)
  if err != nil {
    log.Printf("%T %+v\n", err, err)
  }
  defer f.Close()

  result := make(chan Response_Stat, user)
  defer close(result)

  transport = &http.Transport{
    MaxIdleConnsPerHost: user,
    ResponseHeaderTimeout: 30 * time.Second,
  }

  go func() {
    for {
      transport.CloseIdleConnections()
      time.Sleep(30 * time.Second)
    }
  }()

  client = &http.Client{
    Transport: transport,
  }

  if input == "" {
    queueload(uri, user, trans, result)
  } else {
    infile, err := os.Open(input)
    if err != nil {
      log.Printf("%T %+v\n", err, err)
      return
    }
    defer infile.Close()
    r := bufio.NewReader(infile)
    err = nil
    count := 0
    start := time.Now()
    for err != io.EOF {
      var s string
      s, err = readLine(r)
      if err == nil && len(s) > 0 {
        arr := strings.Split(s, " ")
        if len(arr) != 2 {
          count--
          fmt.Printf("%s have invalid format (doesn't specify transaction)\n", s)
          writeLog(fmt.Sprintf("%s have invalid format (doesn't specify transaction)\r\n", s))
        } else {
          tran, err := strconv.Atoi(arr[1])
          if err != nil {
            continue
          }
          queueload(arr[0], user, tran, result)
        }

        fmt.Println()
        writeLog("\r\n")
        count++
      }
    }
    duration := time.Since(start).Nanoseconds() / time.Millisecond.Nanoseconds()

    fmt.Println("=============== TOTAL SUMMARY ================")
    writeLog("=============== TOTAL SUMMARY ================\r\n")

    fmt.Printf("Total time: %v milliseconds\n", duration)
    writeLog(fmt.Sprintf("Total time: %v milliseconds\r\n", duration))

    fmt.Printf("%v urls tested.\n", count)
    writeLog(fmt.Sprintf("%v urls tested.\r\n", count))

    fmt.Printf("%s DONE\n", time.Now())
    writeLog(fmt.Sprintf("%s DONE\r\n", time.Now().Format(time.RFC850)))

    fmt.Println("=============== END TOTAL SUMMARY ================")
    writeLog("=============== END TOTAL SUMMARY ================\r\n")
  }
}

func queueload(uri string, user int, trans int, result chan Response_Stat) {
  start := time.Now()
  for i := 0 ; i < user ; i++ {
    go sendRequest(uri, trans, result)
  }

  fmt.Printf("%s Start test %s...\n", time.Now().Format(time.RFC850), uri)
  writeLog(fmt.Sprintf("%s Start test %s...\r\n", time.Now().Format(time.RFC850), uri))

  count := 0
  success := 0
  var min_res int64
  var max_res int64
  var sum_res int64
  min_res = 100000
  max_res = 0
  sum_res = 0
  total_data := 0
  timeout := false

  r, err := regexp.Compile("^100|^101|^102|^200|^201|^202|^203|^204|^205|^206|^207|^208|^226|^300|^301|^302|^303|^304|^305|^306|^307|^308")
  if err != nil {
    log.Printf("%T %+v\n", err, err)
    writeLog(fmt.Sprintf("%T %+v\r\n", err, err))
  }

  for ; count != user * trans && !timeout ; {
    select {
    case s := <-result:

      res_time := s.response_time.Nanoseconds() / time.Millisecond.Nanoseconds()
      fmt.Printf("%8d : Status:%s\n           Response time: %7d ms ,Bytes: %v\n", count, s.status, res_time, s.amount_of_data)
      writeLog(fmt.Sprintf("%8d : Status:%s\r\n           Response time: %7d ms ,Bytes: %v\r\n", count, s.status, res_time, s.amount_of_data))

      if r.MatchString(s.status) {
        if(res_time > max_res) {
          max_res = res_time
        }
        if(res_time < min_res) {
          min_res = res_time
        }
        sum_res += res_time
        if s.amount_of_data > 0 {
          total_data += s.amount_of_data
        }
        success++
      }
    }
    count++
  }
  duration := time.Since(start).Nanoseconds() / time.Millisecond.Nanoseconds()

  fmt.Println("=============== SUMMARY ================")
  writeLog("=============== SUMMARY ================\r\n")

  fmt.Printf("%s\n", time.Now().Format(time.RFC850))
  writeLog(fmt.Sprintf("%s\r\n", time.Now().Format(time.RFC850)))

  fmt.Println("Target address:", uri)
  writeLog(fmt.Sprintf("Target address: %v\r\n", uri))

  fmt.Println("Concurrent users:", user)
  writeLog(fmt.Sprintf("Concurrent users: %v \r\n", user))

  fmt.Println("Total transaction:", user * trans)
  writeLog(fmt.Sprintf("Total transaction: %v\r\n", user * trans))

  fmt.Println("Elapsed time:", duration, "milliseconds")
  writeLog(fmt.Sprintf("Elapsed time: %vmilliseconds\r\n", duration))

  fmt.Println("Success transaction:", success)
  writeLog(fmt.Sprintf("Success transaction: %v\r\n", success))

  fmt.Println("Failed transaction:", ( user * trans ) - success)
  writeLog(fmt.Sprintf("Failed transaction: %v\r\n", ( user * trans ) - success))

  fmt.Println("Total response data:", total_data)
  writeLog(fmt.Sprintf("Total response data: %v \r\n", total_data))

  fmt.Println("Transaction rate:", float64( count ) / float64( duration ), "trans/millisec")
  writeLog(fmt.Sprintf("Transaction rate: %v trans/millisec\r\n", float64( count ) / float64( duration )))

  fmt.Printf("Maximum response time: %6d milliseconds\n", max_res)
  writeLog(fmt.Sprintf("Maximum response time: %v milliseconds\r\n", max_res ))

  fmt.Printf("Minimum response time: %6d milliseconds\n", min_res)
  writeLog(fmt.Sprintf("Minimum response time: %v milliseconds\r\n", min_res ))

  fmt.Printf("Average response time: %.4f milliseconds\n", float64( sum_res ) / float64(success))
  writeLog(fmt.Sprintf("Average response time: %.4f milliseconds\r\n", float64( sum_res ) / float64(success)))

  fmt.Println("================= END ==================\n")
  writeLog("================= END ==================\r\n\r\n")
}

func sendRequest(uri string, n int, result chan Response_Stat) {
  for i := 0 ; i < n ; i++ {
    start := time.Now()
    res, err := client.Get(uri)
    response_time := time.Since(start)
    if err != nil {
      result <- Response_Stat{ fmt.Sprintf("Response Error%T %+v", err, err), response_time, 0 }
    } else {
      res.Close = true
      l := int(res.ContentLength)
      result <- Response_Stat{res.Status, response_time, l}
      ioutil.ReadAll(res.Body)
      res.Body.Close()
    }
  }
}

func writeLog(message string) {
  var b bytes.Buffer
  _, err := fmt.Fprintf(&b, message)
  if err != nil {
    return
  }
  _, err = f.Write(b.Bytes())
  if err != nil {
    return
  }
  f.Sync()
}

func readLine(reader *bufio.Reader) (string, error) {
  isPrefix := true
  var err error = nil
  var line, text []byte
  for isPrefix {
    line, isPrefix, err = reader.ReadLine()
    if err != io.EOF && err != nil {
      log.Printf("%T %+v\n", err, err)
    }
    text = append(text, line ...)
  }
  return string(text), err
}
