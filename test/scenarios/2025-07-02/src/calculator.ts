export class Calculator {
	fibonacci(n: number): number {
		if (n <= 1) return n;
		let a = 0,
			b = 1,
			temp: number;
		for (let i = 2; i <= n; i++) {
			temp = a + b;
			a = b;
			b = temp;
		}
		return b;
	}

	primeFactors(n: number): number[] {
		const factors: number[] = [];
		let d = 2;
		while (d * d <= n) {
			while (n % d === 0) {
				factors.push(d);
				n /= d;
			}
			d++;
		}
		if (n > 1) factors.push(n);
		return factors;
	}
}
