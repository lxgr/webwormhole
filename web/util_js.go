// +build js,wasm

// Package util is a set of wrappers for crypto (and other) functions to be
// invoked via Web Assembly.
//
// All functions return nil/null on error.
//
//	msgA = util.start("some pass")
//	[keyB, msgB] = util.exchange("some pass", msgA)
//	keyA = util.finish(msgB)
//	util.open(keyA, util.seal(keyB, "hello"))
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"syscall/js"

	"filippo.io/cpace"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/secretbox"
	"rsc.io/qr"
)

// state is the PAKE state so far.
//
// We can't pass Go pointers to JavaScript, but we need to keep
// the PAKE state (at least for the A side) between invocations.
// We keep it as a single instance variable here, which means an
// instance of this program can only do one A handshake at a time.
// If more is needed this can be changed into a map[something]*cpace.State.
var state *cpace.State

// start(pass string) (base64msgA string)
func start(_ js.Value, args []js.Value) interface{} {
	pass := args[0].String()

	msgA, s, err := cpace.Start(pass, cpace.NewContextInfo("", "", nil))
	if err != nil {
		return nil
	}
	state = s

	return base64.URLEncoding.EncodeToString(msgA)
}

// finish(base64msgB string) (key []byte)
func finish(_ js.Value, args []js.Value) interface{} {
	msgB, err := base64.URLEncoding.DecodeString(args[0].String())
	if err != nil {
		return nil
	}

	mk, err := state.Finish(msgB)
	if err != nil {
		return nil
	}
	hkdf := hkdf.New(sha256.New, mk, nil, nil)
	key := [32]byte{}
	_, err = io.ReadFull(hkdf, key[:])
	if err != nil {
		return nil
	}

	dst := js.Global().Get("Uint8Array").New(32)
	js.CopyBytesToJS(dst, key[:])

	return dst
}

// finish(pass, base64msgA string) (key []byte, base64msgB string)
func exchange(_ js.Value, args []js.Value) interface{} {
	pass := args[0].String()
	msgA, err := base64.URLEncoding.DecodeString(args[1].String())
	if err != nil {
		return []interface{}{nil, nil}
	}

	msgB, mk, err := cpace.Exchange(pass, cpace.NewContextInfo("", "", nil), msgA)
	if err != nil {
		return []interface{}{nil, nil}
	}
	hkdf := hkdf.New(sha256.New, mk, nil, nil)
	key := [32]byte{}
	_, err = io.ReadFull(hkdf, key[:])
	if err != nil {
		return []interface{}{nil, nil}
	}

	dst := js.Global().Get("Uint8Array").New(32)
	js.CopyBytesToJS(dst, key[:])
	return []interface{}{
		dst,
		base64.URLEncoding.EncodeToString(msgB),
	}
}

// open(key []byte, base64ciphertext string) (cleartext string)
func open(_ js.Value, args []js.Value) interface{} {
	var key [32]byte
	js.CopyBytesToGo(key[:], args[0])
	encrypted, err := base64.URLEncoding.DecodeString(args[1].String())
	if err != nil {
		return nil
	}

	var nonce [24]byte
	copy(nonce[:], encrypted[:24])
	clear, ok := secretbox.Open(nil, encrypted[24:], &nonce, &key)
	if !ok {
		return nil
	}

	return string(clear)
}

// open(key []byte, cleartext string) (base64ciphertext string)
func seal(_ js.Value, args []js.Value) interface{} {
	var key [32]byte
	js.CopyBytesToGo(key[:], args[0])
	clear := args[1].String()

	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil
	}

	result := secretbox.Seal(nonce[:], []byte(clear), &nonce, &key)

	return base64.URLEncoding.EncodeToString(result)
}

// qrencode(url string) (png []byte)
func qrencode(_ js.Value, args []js.Value) interface{} {
	code, err := qr.Encode(args[0].String(), qr.L)
	if err != nil {
		return nil
	}
	png := code.PNG()
	dst := js.Global().Get("Uint8Array").New(len(png))
	js.CopyBytesToJS(dst, png)
	return dst
}

func main() {
	js.Global().Set("util", map[string]interface{}{
		"start":    js.FuncOf(start),
		"finish":   js.FuncOf(finish),
		"exchange": js.FuncOf(exchange),
		"open":     js.FuncOf(open),
		"seal":     js.FuncOf(seal),
		"qrencode": js.FuncOf(qrencode),
	})

	// TODO release functions and exit when done.
	select {}
}
