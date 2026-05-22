package utils

// Add returns the sum of two integers.
func Add(x, y int) int {
	return x + y
}

// Subtract returns the difference between two integers.
func Subtract(x, y int) int {
	return x - y
}

// Multiply returns the product of two integers.
func Multiply(x, y int) int {
	return x * y
}

// Divide returns the quotient of two integers.
// If the divisor is zero, it returns 0.
func Divide(x, y int) int {
	if y == 0 {
		return 0
	}
	return x / y
}
