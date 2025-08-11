import { test, expect } from "bun:test";
import { Calculator } from "../src/calculator.js";

test("fibonacci calculations", () => {
	const calc = new Calculator();
	expect(calc.fibonacci(0)).toBe(0);
	expect(calc.fibonacci(1)).toBe(1);
	expect(calc.fibonacci(5)).toBe(5);
	expect(calc.fibonacci(10)).toBe(55);
});

test("prime factorization", () => {
	const calc = new Calculator();
	expect(calc.primeFactors(12)).toEqual([2, 2, 3]);
	expect(calc.primeFactors(17)).toEqual([17]);
	expect(calc.primeFactors(100)).toEqual([2, 2, 5, 5]);
});
