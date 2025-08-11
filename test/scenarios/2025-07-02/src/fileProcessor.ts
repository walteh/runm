import { writeFile, readdir, mkdir } from "fs/promises";
import { join } from "path";

export class FileProcessor {
	private workDir = "/tmp/runm-test-work";

	async createTestFiles(count: number): Promise<void> {
		await mkdir(this.workDir, { recursive: true });

		const promises = Array.from({ length: count }, (_, i) =>
			writeFile(
				join(this.workDir, `test-${i}.json`),
				JSON.stringify(
					{
						id: i,
						timestamp: Date.now(),
						data: Array.from({ length: 10 }, () => Math.random()),
						message: `Test file ${i} generated at ${new Date().toISOString()}`,
					},
					null,
					2
				)
			)
		);

		await Promise.all(promises);
	}

	async countFiles(): Promise<number> {
		try {
			const files = await readdir(this.workDir);
			return files.filter((f) => f.endsWith(".json")).length;
		} catch {
			return 0;
		}
	}
}
