import { Calculator } from "./calculator.js";
import { FileProcessor } from "./fileProcessor.js";

async function main() {
	console.log("🚀 Starting complex test scenario...");

	// Math operations
	const calc = new Calculator();
	const results = [];
	for (let i = 1; i <= 100; i++) {
		results.push(calc.fibonacci(i % 10));
	}
	console.log(`📊 Computed ${results.length} fibonacci numbers, sum: ${results.reduce((a, b) => a + b, 0)}`);

	// File operations
	const processor = new FileProcessor();
	await processor.createTestFiles(50);
	const fileCount = await processor.countFiles();
	console.log(`📁 Created and processed ${fileCount} files`);

	// Network simulation (without actual network calls)
	console.log("🌐 Simulating API calls...");
	const promises = Array.from({ length: 10 }, (_, i) => simulateApiCall(i));
	const responses = await Promise.all(promises);
	console.log(`✅ Processed ${responses.length} simulated API responses`);

	console.log("🎉 Complex test scenario completed successfully!");
}

async function simulateApiCall(id: number): Promise<string> {
	await new Promise((resolve) => setTimeout(resolve, Math.random() * 100));
	return `response-${id}`;
}

main().catch(console.error);
