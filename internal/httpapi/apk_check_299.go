package httpapi

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// Bundle 299 (A): read what the server needs from an uploaded APK, with the
// standard library only: the package name, versionCode and versionName from
// the binary AndroidManifest.xml, and the signing certificate from the APK
// Signing Block (v2 / v3). Android itself checks the signature when the APK
// is installed; the server only makes sure it is LinkUp, newer, and signed
// with the same key as the first APK the owner added.

const apkPackage299 = "com.sajjad.linkup"

type apkInfo299 struct {
	Package     string
	VersionCode int64
	VersionName string
	CertSHA256  string // hex sha256 of the first signer's X.509 certificate
}

// apkProblem299 is a refusal shown to the owner as it is (plain words).
type apkProblem299 struct{ msg string }

func (p apkProblem299) Error() string { return p.msg }

var (
	errNotAPK299    = apkProblem299{"The file is not an APK. Pick the .apk file from the GitHub build."}
	errNoSigner299  = apkProblem299{"This APK has no v2 signature. Use the APK from the same GitHub build."}
	errBadManifest  = apkProblem299{"The APK could not be read (its manifest is damaged). Download it again."}
	errNotLinkUp299 = apkProblem299{"This is not LinkUp. Pick the LinkUp APK."}
)

// readAPK299 reads the APK at path.
func readAPK299(path string) (apkInfo299, error) {
	f, err := os.Open(path)
	if err != nil {
		return apkInfo299{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return apkInfo299{}, err
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return apkInfo299{}, errNotAPK299
	}
	var manifest []byte
	for _, zf := range zr.File {
		if zf.Name != "AndroidManifest.xml" {
			continue
		}
		if zf.UncompressedSize64 > 8<<20 {
			return apkInfo299{}, errBadManifest
		}
		rc, err := zf.Open()
		if err != nil {
			return apkInfo299{}, errBadManifest
		}
		manifest, err = io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return apkInfo299{}, errBadManifest
		}
	}
	if manifest == nil {
		return apkInfo299{}, errNotAPK299
	}
	info, err := parseManifest299(manifest)
	if err != nil {
		return apkInfo299{}, errBadManifest
	}
	cert, err := apkSignerCert299(f, st.Size())
	if err != nil {
		return info, err
	}
	sum := sha256.Sum256(cert)
	info.CertSHA256 = hex.EncodeToString(sum[:])
	return info, nil
}

// ---------------------------------------------------------------- manifest

const (
	resStringPool  = 0x0001
	resXML         = 0x0003
	resXMLStartEl  = 0x0102
	resXMLResMap   = 0x0180
	attrVersionCod = 0x0101021b
	attrVersionNam = 0x0101021c
	typeString     = 0x03
	typeIntDec     = 0x10
	typeIntHex     = 0x11
)

type stringPool299 struct {
	data    []byte
	offsets []uint32
	start   uint32
	utf8    bool
}

func (p stringPool299) get(i uint32) string {
	if i == 0xffffffff || int(i) >= len(p.offsets) {
		return ""
	}
	at := int(p.start + p.offsets[i])
	if at >= len(p.data) {
		return ""
	}
	b := p.data[at:]
	if p.utf8 {
		// utf16 length (1-2 bytes), then utf8 length (1-2 bytes), then bytes.
		skip := func(b []byte) ([]byte, int) {
			if len(b) == 0 {
				return b, 0
			}
			n := int(b[0])
			if n&0x80 != 0 && len(b) > 1 {
				return b[2:], (n&0x7f)<<8 | int(b[1])
			}
			return b[1:], n
		}
		b, _ = skip(b)
		b, n := skip(b)
		if n > len(b) {
			return ""
		}
		return string(b[:n])
	}
	if len(b) < 2 {
		return ""
	}
	n := int(binary.LittleEndian.Uint16(b))
	b = b[2:]
	if n&0x8000 != 0 && len(b) >= 2 {
		n = (n&0x7fff)<<16 | int(binary.LittleEndian.Uint16(b))
		b = b[2:]
	}
	if n*2 > len(b) {
		return ""
	}
	u := make([]uint16, n)
	for k := range u {
		u[k] = binary.LittleEndian.Uint16(b[k*2:])
	}
	return string(utf16.Decode(u))
}

// parseManifest299 reads package / versionCode / versionName from the
// <manifest> element of a binary AndroidManifest.xml.
func parseManifest299(b []byte) (apkInfo299, error) {
	bad := errors.New("bad manifest")
	if len(b) < 8 || binary.LittleEndian.Uint16(b) != resXML {
		return apkInfo299{}, bad
	}
	var pool stringPool299
	var resMap []uint32
	off := int(binary.LittleEndian.Uint16(b[2:]))
	for off+8 <= len(b) {
		ctype := binary.LittleEndian.Uint16(b[off:])
		hsize := int(binary.LittleEndian.Uint16(b[off+2:]))
		csize := int(binary.LittleEndian.Uint32(b[off+4:]))
		if csize < 8 || off+csize > len(b) || hsize > csize {
			return apkInfo299{}, bad
		}
		chunk := b[off : off+csize]
		switch ctype {
		case resStringPool:
			if len(chunk) < 28 {
				return apkInfo299{}, bad
			}
			count := binary.LittleEndian.Uint32(chunk[8:])
			flags := binary.LittleEndian.Uint32(chunk[16:])
			start := binary.LittleEndian.Uint32(chunk[20:])
			if int(hsize)+int(count)*4 > len(chunk) || count > 1<<20 {
				return apkInfo299{}, bad
			}
			pool = stringPool299{data: chunk, start: start, utf8: flags&0x100 != 0, offsets: make([]uint32, count)}
			for i := range pool.offsets {
				pool.offsets[i] = binary.LittleEndian.Uint32(chunk[hsize+i*4:])
			}
		case resXMLResMap:
			for i := hsize; i+4 <= len(chunk); i += 4 {
				resMap = append(resMap, binary.LittleEndian.Uint32(chunk[i:]))
			}
		case resXMLStartEl:
			// node header (16) + ns, name, attrStart(2), attrSize(2), attrCount(2)...
			if len(chunk) < hsize+20 {
				return apkInfo299{}, bad
			}
			ext := chunk[hsize:]
			if pool.get(binary.LittleEndian.Uint32(ext[4:])) != "manifest" {
				break
			}
			attrStart := int(binary.LittleEndian.Uint16(ext[8:]))
			attrSize := int(binary.LittleEndian.Uint16(ext[10:]))
			attrCount := int(binary.LittleEndian.Uint16(ext[12:]))
			if attrSize < 20 {
				return apkInfo299{}, bad
			}
			var out apkInfo299
			for i := 0; i < attrCount; i++ {
				a := attrStart + i*attrSize
				if a+20 > len(ext) {
					return apkInfo299{}, bad
				}
				at := ext[a:]
				nameIdx := binary.LittleEndian.Uint32(at[4:])
				raw := binary.LittleEndian.Uint32(at[8:])
				dtype := at[15]
				data := binary.LittleEndian.Uint32(at[16:])
				name := pool.get(nameIdx)
				var resID uint32
				if int(nameIdx) < len(resMap) {
					resID = resMap[nameIdx]
				}
				str := func() string {
					if raw != 0xffffffff {
						return pool.get(raw)
					}
					if dtype == typeString {
						return pool.get(data)
					}
					return ""
				}
				switch {
				case resID == attrVersionCod || (resID == 0 && name == "versionCode"):
					if dtype == typeIntDec || dtype == typeIntHex {
						out.VersionCode = int64(int32(data))
					}
				case resID == attrVersionNam || (resID == 0 && name == "versionName"):
					out.VersionName = str()
				case name == "package":
					out.Package = str()
				}
			}
			return out, nil
		}
		off += csize
	}
	return apkInfo299{}, bad
}

// ---------------------------------------------------------------- signer

const (
	sigBlockMagic = "APK Sig Block 42"
	sigV2ID       = 0x7109871a
	sigV3ID       = 0xf05368c0
	sigV31ID      = 0x1b93ad61
)

// apkSignerCert299 returns the DER certificate of the first signer from the
// APK Signing Block (v3.1, v3 or v2; all use the same layout up to the
// certificates).
func apkSignerCert299(r io.ReaderAt, size int64) ([]byte, error) {
	// End of central directory: 22 bytes + comment (<= 65535).
	tail := int64(22 + 65535)
	if tail > size {
		tail = size
	}
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return nil, errNotAPK299
	}
	eocd := -1
	for i := len(buf) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) == 0x06054b50 {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		return nil, errNotAPK299
	}
	cdOff := int64(binary.LittleEndian.Uint32(buf[eocd+16:]))
	if cdOff < 32 || cdOff > size {
		return nil, errNoSigner299
	}
	foot := make([]byte, 24)
	if _, err := r.ReadAt(foot, cdOff-24); err != nil {
		return nil, errNoSigner299
	}
	if string(foot[8:]) != sigBlockMagic {
		return nil, errNoSigner299
	}
	blockSize := int64(binary.LittleEndian.Uint64(foot))
	start := cdOff - blockSize - 8
	if blockSize < 24 || blockSize > 64<<20 || start < 0 {
		return nil, errNoSigner299
	}
	block := make([]byte, blockSize-24+8) // size field + pairs
	if _, err := r.ReadAt(block, start); err != nil {
		return nil, errNoSigner299
	}
	if int64(binary.LittleEndian.Uint64(block)) != blockSize {
		return nil, errNoSigner299
	}
	values := map[uint32][]byte{}
	for p := block[8:]; len(p) >= 12; {
		n := binary.LittleEndian.Uint64(p)
		if n < 4 || n > uint64(len(p)-8) {
			return nil, errNoSigner299
		}
		id := binary.LittleEndian.Uint32(p[8:])
		values[id] = p[12 : 8+n]
		p = p[8+n:]
	}
	for _, id := range []uint32{sigV31ID, sigV3ID, sigV2ID} {
		if v, ok := values[id]; ok {
			if cert, err := firstCert299(v); err == nil {
				return cert, nil
			}
		}
	}
	return nil, errNoSigner299
}

// lp reads one uint32 length-prefixed field.
func lp299(b []byte) (field, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("short")
	}
	n := binary.LittleEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, errors.New("short")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// firstCert299: signers -> signer -> signed data -> (digests, certificates) -> cert.
func firstCert299(v []byte) ([]byte, error) {
	signers, _, err := lp299(v)
	if err != nil {
		return nil, err
	}
	signer, _, err := lp299(signers)
	if err != nil {
		return nil, err
	}
	signed, _, err := lp299(signer)
	if err != nil {
		return nil, err
	}
	_, rest, err := lp299(signed) // digests
	if err != nil {
		return nil, err
	}
	certs, _, err := lp299(rest)
	if err != nil {
		return nil, err
	}
	cert, _, err := lp299(certs)
	if err != nil {
		return nil, err
	}
	if _, err := x509.ParseCertificate(cert); err != nil {
		return nil, err
	}
	return cert, nil
}

// checkAPK299 applies the owner's rules. currentBuild 0 = no APK yet;
// firstCert "" = no APK was ever added (this one sets the key).
func checkAPK299(info apkInfo299, currentBuild int64, currentVersion, firstCert string) error {
	if info.Package != apkPackage299 {
		return errNotLinkUp299
	}
	if info.VersionCode <= 0 {
		return errBadManifest
	}
	if currentBuild > 0 && info.VersionCode <= currentBuild {
		name := info.VersionName
		if name == "" {
			name = "this APK"
		}
		return apkProblem299{fmt.Sprintf("This APK is LinkUp %s (build %d). It must be newer than %s (build %d).",
			name, info.VersionCode, currentVersion, currentBuild)}
	}
	if firstCert != "" && !strings.EqualFold(firstCert, info.CertSHA256) {
		return apkProblem299{"This APK is signed with a different key. Use the APK from the same GitHub build."}
	}
	return nil
}

// fileSHA256 hashes a file (hex).
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
