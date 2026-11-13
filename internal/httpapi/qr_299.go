package httpapi

import (
	"errors"
	"fmt"
	"strings"
)

// Bundle 299 (A, B): a small QR code maker (byte mode, error level M,
// versions 1-10, up to 213 bytes) so the server can show the download link
// as a QR with no outside module. It follows the QR standard (ISO 18004):
// Reed-Solomon over GF(256) 0x11D, the 8 masks with the standard penalty.

type qrSpec299 struct{ ec, b1, d1, b2, d2 int }

// Error level M: EC codewords per block, group 1 (blocks, data), group 2.
var qrM299 = [...]qrSpec299{
	{}, {10, 1, 16, 0, 0}, {16, 1, 28, 0, 0}, {26, 1, 44, 0, 0}, {18, 2, 32, 0, 0}, {24, 2, 43, 0, 0},
	{16, 4, 27, 0, 0}, {18, 4, 31, 0, 0}, {22, 2, 38, 2, 39}, {22, 3, 36, 2, 37}, {26, 4, 43, 1, 44},
}

var qrAlign299 = [...][]int{{}, {}, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34}, {6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50}}

type qr299 struct {
	size int
	dark [][]bool
	fn   [][]bool
}

func gfMul299(x, y byte) byte {
	z := 0
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>uint(i))&1) * int(x)
	}
	return byte(z)
}

func rsDivisor299(degree int) []byte {
	r := make([]byte, degree)
	r[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := 0; j < degree; j++ {
			r[j] = gfMul299(r[j], root)
			if j+1 < degree {
				r[j] ^= r[j+1]
			}
		}
		root = gfMul299(root, 2)
	}
	return r
}

func rsRemainder299(data, div []byte) []byte {
	r := make([]byte, len(div))
	for _, b := range data {
		f := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i := range r {
			r[i] ^= gfMul299(div[i], f)
		}
	}
	return r
}

// qrEncode299 makes the QR for text.
func qrEncode299(text string) (*qr299, error) {
	data := []byte(text)
	ver := 0
	for v := 1; v <= 10; v++ {
		s := qrM299[v]
		capBits := (s.b1*s.d1 + s.b2*s.d2) * 8
		cc := 8
		if v >= 10 {
			cc = 16
		}
		if 4+cc+len(data)*8 <= capBits {
			ver = v
			break
		}
	}
	if ver == 0 {
		return nil, errors.New("text too long for a QR")
	}
	s := qrM299[ver]
	capBytes := s.b1*s.d1 + s.b2*s.d2
	// Bit stream.
	var bits []bool
	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, (v>>uint(i))&1 == 1)
		}
	}
	put(0b0100, 4)
	if ver >= 10 {
		put(len(data), 16)
	} else {
		put(len(data), 8)
	}
	for _, b := range data {
		put(int(b), 8)
	}
	for i := 0; i < 4 && len(bits) < capBytes*8; i++ {
		bits = append(bits, false)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	cw := make([]byte, 0, capBytes)
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			if bits[i+j] {
				b |= 1 << uint(7-j)
			}
		}
		cw = append(cw, b)
	}
	for pad := byte(0xEC); len(cw) < capBytes; {
		cw = append(cw, pad)
		pad ^= 0xEC ^ 0x11
	}
	// Blocks + EC, then interleave.
	div := rsDivisor299(s.ec)
	var blocks, ecs [][]byte
	k := 0
	for g, n := range []int{s.b1, s.b2} {
		d := s.d1
		if g == 1 {
			d = s.d2
		}
		for i := 0; i < n; i++ {
			blk := cw[k : k+d]
			k += d
			blocks = append(blocks, blk)
			ecs = append(ecs, rsRemainder299(blk, div))
		}
	}
	var out []byte
	maxD := s.d1
	if s.d2 > maxD {
		maxD = s.d2
	}
	for i := 0; i < maxD; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := 0; i < s.ec; i++ {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	q := &qr299{size: ver*4 + 17}
	q.dark = make([][]bool, q.size)
	q.fn = make([][]bool, q.size)
	for i := range q.dark {
		q.dark[i] = make([]bool, q.size)
		q.fn[i] = make([]bool, q.size)
	}
	q.functionPatterns(ver)
	q.placeData(out)
	best, bestScore := 0, -1
	for m := 0; m < 8; m++ {
		q.applyMask(m)
		q.formatBits(m)
		if sc := q.penalty(); bestScore < 0 || sc < bestScore {
			best, bestScore = m, sc
		}
		q.applyMask(m) // undo (XOR)
	}
	q.applyMask(best)
	q.formatBits(best)
	return q, nil
}

func (q *qr299) set(x, y int, dark bool) {
	q.dark[y][x] = dark
	q.fn[y][x] = true
}

func (q *qr299) functionPatterns(ver int) {
	n := q.size
	for i := 0; i < n; i++ {
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	finder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := cx+dx, cy+dy
				if x < 0 || y < 0 || x >= n || y >= n {
					continue
				}
				d := abs299(dx)
				if abs299(dy) > d {
					d = abs299(dy)
				}
				q.set(x, y, d != 2 && d != 4)
			}
		}
	}
	finder(3, 3)
	finder(n-4, 3)
	finder(3, n-4)
	al := qrAlign299[ver]
	for i, ay := range al {
		for j, ax := range al {
			if (i == 0 && j == 0) || (i == 0 && j == len(al)-1) || (i == len(al)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					d := abs299(dx)
					if abs299(dy) > d {
						d = abs299(dy)
					}
					q.set(ax+dx, ay+dy, d != 1)
				}
			}
		}
	}
	q.formatBits(0) // reserve
	if ver >= 7 {
		rem := ver
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		bits := ver<<12 | rem
		for i := 0; i < 18; i++ {
			b := (bits>>uint(i))&1 == 1
			a, c := n-11+i%3, i/3
			q.set(a, c, b)
			q.set(c, a, b)
		}
	}
}

// formatBits: level M = 0b00.
func (q *qr299) formatBits(mask int) {
	data := 0<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>uint(i))&1 == 1 }
	n := q.size
	for i := 0; i <= 5; i++ {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		q.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, n-15+i, bit(i))
	}
	q.set(8, n-8, true)
}

func (q *qr299) placeData(data []byte) {
	n := q.size
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < n; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = n - 1 - vert
				}
				if !q.fn[y][x] && i < len(data)*8 {
					q.dark[y][x] = (data[i>>3]>>uint(7-(i&7)))&1 == 1
					i++
				}
			}
		}
	}
}

func (q *qr299) applyMask(m int) {
	for y := 0; y < q.size; y++ {
		for x := 0; x < q.size; x++ {
			if q.fn[y][x] {
				continue
			}
			var inv bool
			switch m {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			default:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				q.dark[y][x] = !q.dark[y][x]
			}
		}
	}
}

// penalty: the standard rules N1 (runs), N2 (2x2), N3 (finder-like), N4 (balance).
func (q *qr299) penalty() int {
	n := q.size
	score := 0
	at := func(x, y int, col bool) bool {
		if col {
			return q.dark[x][y]
		}
		return q.dark[y][x]
	}
	for _, col := range []bool{false, true} {
		for y := 0; y < n; y++ {
			run := 1
			for x := 1; x < n; x++ {
				if at(x, y, col) == at(x-1, y, col) {
					run++
					if run == 5 {
						score += 3
					} else if run > 5 {
						score++
					}
				} else {
					run = 1
				}
			}
			for x := 0; x+10 < n; x++ {
				p := [11]bool{}
				for k := range p {
					p[k] = at(x+k, y, col)
				}
				a := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
				b := [11]bool{false, false, false, false, true, false, true, true, true, false, true}
				if p == a || p == b {
					score += 40
				}
			}
		}
	}
	darkCount := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if q.dark[y][x] {
				darkCount++
			}
			if x+1 < n && y+1 < n {
				c := q.dark[y][x]
				if q.dark[y][x+1] == c && q.dark[y+1][x] == c && q.dark[y+1][x+1] == c {
					score += 3
				}
			}
		}
	}
	total := n * n
	k := (abs299(darkCount*20-total*10) + total - 1) / total
	return score + (k-1)*10
}

func abs299(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// qrSVG299 draws the QR as a small SVG (white back, 4-module quiet zone).
func qrSVG299(text string) string {
	q, err := qrEncode299(text)
	if err != nil {
		return ""
	}
	n := q.size + 8
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR code"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < q.size; y++ {
		for x := 0; x < q.size; x++ {
			if q.dark[y][x] {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}
