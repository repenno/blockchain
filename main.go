package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

// Block represents each 'item' in the blockchain
type Block struct {
	Index     int
	Timestamp string
	FileHash  string
	Event     string
	EventTime string
	Location  string
	Server    string
	Hash      string
	PrevHash  string
}

// Blockchain is a series of validated Blocks
var Blockchain []Block

var BlockMap map[string]*Block

// Message takes incoming JSON payload for writing hash
type CreateBlockReq struct {
	FileHash  string
	Event     string
	EventTime string
	Location  string
	Server    string
}

//"FileHash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
//"Event": "unathorized access",
//"EventTime": "2018-04-23T18:25:43.511Z",
//"Location" : "San Jose, CA",
//"Server" : "vpn-1-sjc.ssl.cisco.com"

type ValidationReq struct {
	CreateMessage CreateBlockReq
	Hash          string
}

type ValidationResp struct {
	ValidationMessage ValidationReq
	Result            bool
}

var mutex = &sync.Mutex{}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	BlockMap = make(map[string]*Block)

	go func() {
		t := time.Now()
		genesisBlock := Block{}
		genesisBlock = Block{0, t.String(), "", "", "", "", "", calculateHash(genesisBlock), ""}
		spew.Dump(genesisBlock)

		mutex.Lock()
		Blockchain = append(Blockchain, genesisBlock)
		mutex.Unlock()
	}()
	log.Fatal(run())

}

// web server
func run() error {
	mux := makeMuxRouter()
	httpPort := os.Getenv("PORT")
	log.Println("HTTP Server Listening on port :", httpPort)
	s := &http.Server{
		Addr:           ":" + httpPort,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

// create handlers
func makeMuxRouter() http.Handler {
	muxRouter := mux.NewRouter()
	muxRouter.HandleFunc("/", handleGetBlockchain).Methods("GET")
	muxRouter.HandleFunc("/validation", handleValidation).Methods("POST")
	muxRouter.HandleFunc("/block/{hash}", handleGetOneBlockChain).Methods("GET")
	muxRouter.HandleFunc("/block", handleWriteBlock).Methods("POST")
	return muxRouter
}

// takes JSON payload as an input for log (fileHash)
func handleValidation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var v ValidationReq
	var vResp ValidationResp
	var valid bool
	var status = http.StatusBadRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&v); err != nil {
		respondWithJSON(w, r, status, r.Body)
		return
	}
	defer r.Body.Close()

	if block, ok := BlockMap[v.Hash]; ok {
		if strings.Compare(block.Event, v.CreateMessage.Event) == 0 {
			valid = true
			status = http.StatusCreated
		}
	}

	vResp.ValidationMessage = v
	vResp.Result = valid

	respondWithJSON(w, r, status, vResp)

}

// Get a specific Block
func handleGetOneBlockChain(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileHash := vars["hash"]
	block := BlockMap[fileHash]
	// We pass pointed to block but Marshall converts the actual
	// object
	bytes, err := json.MarshalIndent(block, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, string(bytes))

}

// get blockchain when we receive an http request
func handleGetBlockchain(w http.ResponseWriter, r *http.Request) {
	bytes, err := json.MarshalIndent(Blockchain, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, string(bytes))
}

// takes JSON payload as an input for log (fileHash)
func handleWriteBlock(w http.ResponseWriter, r *http.Request) {
	// w.Header().Set("Content-Type", "application/json")
	var m CreateBlockReq
	var statusCode = http.StatusCreated
	var newBlock Block

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&m); err != nil {
		respondWithJSON(w, r, http.StatusBadRequest, r.Body)
		return
	}
	defer r.Body.Close()

	if len(m.Event) != 0 {

		mutex.Lock()
		newBlock = generateBlock(Blockchain[len(Blockchain)-1], m.FileHash, m.Event, m.EventTime, m.Location, m.Server)
		mutex.Unlock()

		if isBlockValid(newBlock, Blockchain[len(Blockchain)-1]) {
			Blockchain = append(Blockchain, newBlock)
			spew.Dump(Blockchain)
		}

		// Add block to hash map so it can be searched in O(1)
		BlockMap[newBlock.Hash] = &newBlock
	} else {
		statusCode = http.StatusBadRequest
	}

	respondWithJSON(w, r, statusCode, newBlock)

}

func respondWithJSON(w http.ResponseWriter, r *http.Request, code int, payload interface{}) {
	response, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("HTTP 500: Internal Server Error"))
		return
	}
	w.WriteHeader(code)
	w.Write(response)
}

// make sure block is valid by checking index, and comparing the hash of the previous block
func isBlockValid(newBlock, oldBlock Block) bool {
	if oldBlock.Index+1 != newBlock.Index {
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		return false
	}

	if calculateHash(newBlock) != newBlock.Hash {
		return false
	}

	return true
}

// SHA256 hasing
func calculateHash(block Block) string {
	record := strconv.Itoa(block.Index) + block.Timestamp + block.FileHash + block.Event + block.EventTime + block.Location + block.Server + block.PrevHash
	h := sha256.New()
	h.Write([]byte(record))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

// create a new block using previous block's hash
func generateBlock(oldBlock Block, fileHash string, event string, eventTime string, location string, server string) Block {

	var newBlock Block

	t := time.Now()

	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = t.String()
	newBlock.FileHash = fileHash
	newBlock.Event = event
	newBlock.EventTime = eventTime
	newBlock.Location = location
	newBlock.Server = server
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Hash = calculateHash(newBlock)

	return newBlock
}
