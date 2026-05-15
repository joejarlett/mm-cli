/**
 * mm stt — speech-to-text via the hub's whisper-stt service.
 *
 * Usage:
 *   mm stt <file>            Print transcribed text
 *   mm stt - < audio.wav     Read audio bytes from stdin
 *   mm stt file --json       Full {text, duration_s, infer_ms} JSON
 *
 * Accepts any audio format whisper-stt-native can decode — WAV via
 * the fast path, mp3/m4a/ogg/flac/webm via the server-side ffmpeg
 * fallback.
 */

import { readFile } from 'node:fs/promises';
import { loadAuth } from '../auth';

const HUB_URL = process.env.MM_HUB_URL || 'https://meta-me.uk';

export async function sttDispatch(args: string[], flags: { json?: boolean }) {
	const file = args[0];
	if (!file) {
		console.error('Usage: mm stt <file>   (use "-" for stdin)');
		process.exit(1);
	}

	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const audio: Buffer = file === '-' ? await readStdin() : await readFile(file);
	if (!audio.byteLength) throw new Error('audio input is empty');

	const res = await fetch(`${HUB_URL}/api/stt/transcribe`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${auth.token}`,
			'Content-Type': 'application/octet-stream'
		},
		// `Buffer` is a Uint8Array so it slots straight into BodyInit.
		body: audio
	});

	if (!res.ok) {
		const detail = await res.text().catch(() => res.statusText);
		throw new Error(`STT failed (${res.status}): ${detail.slice(0, 200)}`);
	}

	const data = (await res.json()) as { text: string; duration_s: number; infer_ms: number };
	if (flags.json) {
		console.log(JSON.stringify(data));
	} else {
		console.log(data.text);
	}
}

async function readStdin(): Promise<Buffer> {
	const chunks: Buffer[] = [];
	for await (const chunk of process.stdin) {
		chunks.push(typeof chunk === 'string' ? Buffer.from(chunk) : (chunk as Buffer));
	}
	return Buffer.concat(chunks);
}
