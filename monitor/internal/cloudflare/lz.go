//go:build solver

package cloudflare

// LZ-String compression/decompression with custom alphabet support.
// Original source: github.com/lazarus/lz-string-go

import (
	"errors"
	"math"
	"strings"
	"sync"
	"unicode/utf8"
)

const defaultKeyStrBase64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="

// LZCompress compresses input using LZ-String with the given alphabet.
// If alphabet is empty, the default base64 alphabet is used.
func LZCompress(uncompressed string, alphabet string) string {
	if len(uncompressed) == 0 {
		return ""
	}
	if alphabet == "" {
		alphabet = defaultKeyStrBase64
	}
	charArr := []rune(alphabet)
	res := lzCompress([]rune(uncompressed), 6, charArr)
	switch len(res) % 4 {
	case 3:
		return res + "="
	case 2:
		return res + "=="
	case 1:
		return res + "==="
	}
	return res
}

func lzCompress(uncompressed []rune, bitsPerChar int, charArr []rune) string {
	if len(uncompressed) == 0 {
		return ""
	}

	var value int
	contextDictionary := make(map[string]int)
	contextDictionaryToCreate := make(map[string]bool)
	var contextC string
	var contextW string
	var contextWc string
	contextEnlargeIn := int64(2)
	contextDictSize := 3
	contextNumBits := 2
	var contextDataString strings.Builder
	contextDataVal := 0
	contextDataPosition := 0

	for ii := 0; ii < len(uncompressed); ii++ {
		contextC = string(uncompressed[ii])
		if _, in := contextDictionary[contextC]; !in {
			contextDictionary[contextC] = contextDictSize
			contextDictSize++
			contextDictionaryToCreate[contextC] = true
		}
		contextWc = contextW + contextC
		if _, in := contextDictionary[contextWc]; in {
			contextW = contextWc
		} else {
			if _, in := contextDictionaryToCreate[contextW]; in {
				contextWRune := int([]rune(contextW)[0])
				if contextWRune < 256 {
					for i := 0; i < contextNumBits; i++ {
						contextDataVal <<= 1
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextDataString.WriteRune(charArr[contextDataVal])
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
					}
					value = contextWRune
					for i := 0; i < 8; i++ {
						contextDataVal = (contextDataVal << 1) | (value & 1)
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextDataString.WriteRune(charArr[contextDataVal])
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value >>= 1
					}
				} else {
					value = 1
					for i := 0; i < contextNumBits; i++ {
						contextDataVal = (contextDataVal << 1) | value
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextDataString.WriteRune(charArr[contextDataVal])
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value = 0
					}
					value = contextWRune
					for i := 0; i < 16; i++ {
						contextDataVal = (contextDataVal << 1) | (value & 1)
						if contextDataPosition == bitsPerChar-1 {
							contextDataPosition = 0
							contextDataString.WriteRune(charArr[contextDataVal])
							contextDataVal = 0
						} else {
							contextDataPosition++
						}
						value >>= 1
					}
				}
				contextEnlargeIn--
				if contextEnlargeIn == 0 {
					contextEnlargeIn = int64(math.Pow(2, float64(contextNumBits)))
					contextNumBits++
				}
				delete(contextDictionaryToCreate, contextW)
			} else {
				value = contextDictionary[contextW]
				for i := 0; i < contextNumBits; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextDataString.WriteRune(charArr[contextDataVal])
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value >>= 1
				}
			}
			contextEnlargeIn--
			if contextEnlargeIn == 0 {
				contextEnlargeIn = int64(math.Pow(2, float64(contextNumBits)))
				contextNumBits++
			}
			contextDictionary[contextWc] = contextDictSize
			contextDictSize++
			contextW = contextC
		}
	}

	if contextW != "" {
		if _, in := contextDictionaryToCreate[contextW]; in {
			contextWRune := int([]rune(contextW)[0])
			if contextWRune < 256 {
				for i := 0; i < contextNumBits; i++ {
					contextDataVal <<= 1
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextDataString.WriteRune(charArr[contextDataVal])
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
				}
				value = contextWRune
				for i := 0; i < 8; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextDataString.WriteRune(charArr[contextDataVal])
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value >>= 1
				}
			} else {
				value = 1
				for i := 0; i < contextNumBits; i++ {
					contextDataVal = (contextDataVal << 1) | value
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextDataString.WriteRune(charArr[contextDataVal])
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value = 0
				}
				value = contextWRune
				for i := 0; i < 16; i++ {
					contextDataVal = (contextDataVal << 1) | (value & 1)
					if contextDataPosition == bitsPerChar-1 {
						contextDataPosition = 0
						contextDataString.WriteRune(charArr[contextDataVal])
						contextDataVal = 0
					} else {
						contextDataPosition++
					}
					value >>= 1
				}
			}
			contextEnlargeIn--
			if contextEnlargeIn == 0 {
				contextEnlargeIn = int64(math.Pow(2, float64(contextNumBits)))
				contextNumBits++
			}
			delete(contextDictionaryToCreate, contextW)
		} else {
			value = contextDictionary[contextW]
			for i := 0; i < contextNumBits; i++ {
				contextDataVal = (contextDataVal << 1) | (value & 1)
				if contextDataPosition == bitsPerChar-1 {
					contextDataPosition = 0
					contextDataString.WriteRune(charArr[contextDataVal])
					contextDataVal = 0
				} else {
					contextDataPosition++
				}
				value >>= 1
			}
		}
		contextEnlargeIn--
		if contextEnlargeIn == 0 {
			contextEnlargeIn = int64(math.Pow(2, float64(contextNumBits)))
			contextNumBits++
		}
	}

	value = 2
	for i := 0; i < contextNumBits; i++ {
		contextDataVal = (contextDataVal << 1) | (value & 1)
		if contextDataPosition == bitsPerChar-1 {
			contextDataPosition = 0
			contextDataString.WriteRune(charArr[contextDataVal])
			contextDataVal = 0
		} else {
			contextDataPosition++
		}
		value >>= 1
	}

	for {
		contextDataVal <<= 1
		if contextDataPosition == bitsPerChar-1 {
			contextDataString.WriteRune(charArr[contextDataVal])
			break
		} else {
			contextDataPosition++
		}
	}
	return contextDataString.String()
}

// LZDecompress decompresses LZ-String data with the given alphabet.
var baseReverseDic sync.Map

type decompressState struct {
	input      string
	alphabet   string
	val        int
	position   int
	index      int
	dictionary []string
	enlargeIn  float64
	numBits    int
}

func buildReverseDic(alphabet string) map[byte]int {
	val := make(map[byte]int)
	charArr := []rune(alphabet)
	for i := 0; i < len(charArr); i++ {
		val[byte(charArr[i])] = i
	}
	return val
}

func getBaseValue(alphabet string, char byte) int {
	vv, ok := baseReverseDic.Load(alphabet)
	var arr map[byte]int
	if ok {
		arr = vv.(map[byte]int)
	} else {
		arr = buildReverseDic(alphabet)
		baseReverseDic.Store(alphabet, arr)
	}
	return arr[char]
}

func readBits(nb int, data *decompressState) int {
	result := 0
	power := 1
	for i := 0; i < nb; i++ {
		respB := data.val & data.position
		data.position /= 2
		if data.position == 0 {
			data.position = 32
			data.val = getBaseValue(data.alphabet, data.input[data.index])
			data.index++
		}
		if respB > 0 {
			result |= power
		}
		power *= 2
	}
	return result
}

func appendDictValue(data *decompressState, str string) {
	data.dictionary = append(data.dictionary, str)
	data.enlargeIn--
	if data.enlargeIn == 0 {
		data.enlargeIn = math.Pow(2, float64(data.numBits))
		data.numBits++
	}
}

func getString(last string, data *decompressState) (string, bool, error) {
	c := readBits(data.numBits, data)
	switch c {
	case 0:
		str := string(rune(readBits(8, data)))
		appendDictValue(data, str)
		return str, false, nil
	case 1:
		str := string(rune(readBits(16, data)))
		appendDictValue(data, str)
		return str, false, nil
	case 2:
		return "", true, nil
	}
	if c < len(data.dictionary) {
		return data.dictionary[c], false, nil
	}
	if c == len(data.dictionary) {
		return concatWithFirstRune(last, last), false, nil
	}
	return "", false, errors.New("bad character encoding")
}

func concatWithFirstRune(str string, getFirstRune string) string {
	r, _ := utf8.DecodeRuneInString(getFirstRune)
	return str + string(r)
}

// LZDecompress decompresses LZ-String data with the given alphabet.
// If alphabet is empty, the default base64 alphabet is used.
func LZDecompress(input string, alphabet string) (string, error) {
	if len(input) == 0 {
		return "", nil
	}
	if alphabet == "" {
		alphabet = defaultKeyStrBase64
	}
	data := decompressState{
		input:      input,
		alphabet:   alphabet,
		val:        getBaseValue(alphabet, input[0]),
		position:   32,
		index:      1,
		dictionary: []string{"0", "1", "2"},
		enlargeIn:  5,
		numBits:    2,
	}
	result, isEnd, err := getString("", &data)
	if err != nil || isEnd {
		return result, err
	}
	last := result
	data.numBits++
	for {
		str, isEnd, err := getString(last, &data)
		if err != nil || isEnd {
			return result, err
		}
		result += str
		appendDictValue(&data, concatWithFirstRune(last, str))
		last = str
	}
}
