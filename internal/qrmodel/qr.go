// Package qrmodel independently ports the source-owned previous production application QR Model 2
// byte-mode encoder (versions 1-12, ECC L). No external packages or network.
// Matrix includes a four-module white quiet zone on all sides.
package qrmodel

import "errors"

var ErrTooLong = errors.New("QR payload must be 1 to 319 bytes")
var ecc = [13]int{0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24}
var blocks = [13]int{0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4}

type encoder struct {
	n               int
	pixel, reserved [][]bool
}

func (q *encoder) place(x, y int, black, locked bool) {
	if x < 0 || y < 0 || x >= q.n || y >= q.n {
		return
	}
	q.pixel[y][x] = black
	if locked {
		q.reserved[y][x] = true
	}
}
func bit(n, i int) bool { return ((n >> i) & 1) != 0 }
func (q *encoder) format() {
	data := 8
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := ((data << 10) | rem) ^ 0x5412
	for i := 0; i <= 5; i++ {
		q.place(8, i, bit(bits, i), true)
	}
	q.place(8, 7, bit(bits, 6), true)
	q.place(8, 8, bit(bits, 7), true)
	q.place(7, 8, bit(bits, 8), true)
	for i := 9; i < 15; i++ {
		q.place(14-i, 8, bit(bits, i), true)
	}
	for i := 0; i < 8; i++ {
		q.place(q.n-1-i, 8, bit(bits, i), true)
	}
	for i := 8; i < 15; i++ {
		q.place(8, q.n-15+i, bit(bits, i), true)
	}
	q.place(8, q.n-8, true, true)
}
func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}
func (q *encoder) finder(cx, cy int) {
	for y := -4; y <= 4; y++ {
		for x := -4; x <= 4; x++ {
			r := abs(x)
			if abs(y) > r {
				r = abs(y)
			}
			q.place(cx+x, cy+y, r <= 1 || r == 3, true)
		}
	}
}
func (q *encoder) align(cx, cy int) {
	for y := -2; y <= 2; y++ {
		for x := -2; x <= 2; x++ {
			r := abs(x)
			if abs(y) > r {
				r = abs(y)
			}
			q.place(cx+x, cy+y, r != 1, true)
		}
	}
}
func (q *encoder) functions(version int) {
	for i := 0; i < q.n; i++ {
		q.place(6, i, i%2 == 0, true)
		q.place(i, 6, i%2 == 0, true)
	}
	q.finder(3, 3)
	q.finder(q.n-4, 3)
	q.finder(3, q.n-4)
	if version >= 2 {
		count := version/7 + 2
		step := ((version*4 + count*2 + 1) / (count*2 - 2)) * 2
		if version == 32 {
			step = 26
		}
		pos := make([]int, count)
		pos[0] = 6
		for i := count - 1; i >= 1; i-- {
			pos[i] = q.n - 7 - (count-1-i)*step
		}
		for y := 0; y < count; y++ {
			for x := 0; x < count; x++ {
				if (x == 0 && y == 0) || (x == 0 && y == count-1) || (x == count-1 && y == 0) {
					continue
				}
				q.align(pos[x], pos[y])
			}
		}
	}
	for i := 0; i < 9; i++ {
		q.place(8, i, false, true)
		q.place(i, 8, false, true)
	}
	q.place(8, 6, true, true)
	q.place(6, 8, true, true)
	for i := 0; i < 8; i++ {
		q.place(q.n-1-i, 8, false, true)
		q.place(8, q.n-1-i, false, true)
	}
	if version >= 7 {
		bits := version
		for i := 0; i < 12; i++ {
			bits = (bits << 1) ^ ((bits >> 11) * 0x1f25)
		}
		bits = (version << 12) | bits
		for i := 0; i < 18; i++ {
			b := bit(bits, i)
			a := q.n - 11 + i%3
			c := i / 3
			q.place(a, c, b, true)
			q.place(c, a, b, true)
		}
	}
	q.format()
}
func multiply(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x1d)
		if ((y >> i) & 1) != 0 {
			z ^= x
		}
	}
	return z
}
func divisor(degree int) []byte {
	out := make([]byte, degree)
	out[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := 0; j < degree; j++ {
			out[j] = multiply(out[j], root)
			if j+1 < degree {
				out[j] ^= out[j+1]
			}
		}
		root = multiply(root, 2)
	}
	return out
}
func remainder(data, div []byte) []byte {
	out := make([]byte, len(div))
	for _, d := range data {
		f := d ^ out[0]
		copy(out, out[1:])
		out[len(out)-1] = 0
		for j := range out {
			out[j] ^= multiply(div[j], f)
		}
	}
	return out
}
func rawbits(v int) int {
	r := (16*v+128)*v + 64
	if v >= 2 {
		n := v/7 + 2
		r -= (25*n-10)*n - 55
		if v >= 7 {
			r -= 36
		}
	}
	return r
}
func appendBits(data []byte, bitpos *int, n, count int) {
	for i := count - 1; i >= 0; i-- {
		if bit(n, i) {
			data[*bitpos>>3] |= 1 << uint(7-(*bitpos&7))
		}
		*bitpos++
	}
}
func Matrix(text string) ([][]bool, error) {
	size := len(text)
	if size == 0 || size > 319 {
		return nil, ErrTooLong
	}
	version, databytes := 0, 0
	for v := 1; v <= 12; v++ {
		b := rawbits(v)/8 - blocks[v]*ecc[v]
		cc := 8
		if v >= 10 {
			cc = 16
		}
		if 4+cc+size*8 <= b*8 {
			version = v
			databytes = b
			break
		}
	}
	if version == 0 {
		return nil, ErrTooLong
	}
	n := version*4 + 17
	q := encoder{n: n, pixel: make([][]bool, n), reserved: make([][]bool, n)}
	for y := 0; y < n; y++ {
		q.pixel[y] = make([]bool, n)
		q.reserved[y] = make([]bool, n)
	}
	data := make([]byte, databytes)
	pos := 0
	cc := 8
	if version >= 10 {
		cc = 16
	}
	appendBits(data, &pos, 4, 4)
	appendBits(data, &pos, size, cc)
	for _, b := range []byte(text) {
		appendBits(data, &pos, int(b), 8)
	}
	for i := 0; i < 4 && pos < databytes*8; i++ {
		appendBits(data, &pos, 0, 1)
	}
	for pos%8 != 0 {
		appendBits(data, &pos, 0, 1)
	}
	start := pos / 8
	for i := start; i < databytes; i++ {
		if (i-start)%2 == 0 {
			data[i] = 0xec
		} else {
			data[i] = 0x11
		}
	}
	nblocks := blocks[version]
	degree := ecc[version]
	rawbytes := rawbits(version) / 8
	shortblocks := nblocks - rawbytes%nblocks
	shortdata := rawbytes/nblocks - degree
	output := make([]byte, rawbytes)
	div := divisor(degree)
	off := 0
	for i := 0; i < nblocks; i++ {
		blen := shortdata
		if i >= shortblocks {
			blen++
		}
		rem := remainder(data[off:off+blen], div)
		for j, k := 0, i; j < blen; j, k = j+1, k+nblocks {
			if j == shortdata {
				k -= shortblocks
			}
			output[k] = data[off+j]
		}
		for j, k := 0, databytes+i; j < degree; j, k = j+1, k+nblocks {
			output[k] = rem[j]
		}
		off += blen
	}
	q.functions(version)
	p := 0
	up := true
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for j := 0; j < n; j++ {
			y := j
			if up {
				y = n - 1 - j
			}
			for k := 0; k < 2; k++ {
				x := right - k
				if q.reserved[y][x] {
					continue
				}
				black := false
				if p < rawbytes*8 {
					black = bit(int(output[p>>3]), 7-(p&7))
				}
				if ((x + y) & 1) == 0 {
					black = !black
				}
				q.place(x, y, black, false)
				p++
			}
		}
		up = !up
	}
	result := make([][]bool, n+8)
	for y := range result {
		result[y] = make([]bool, n+8)
		if y >= 4 && y < n+4 {
			copy(result[y][4:], q.pixel[y-4])
		}
	}
	return result, nil
}
