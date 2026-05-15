/**
 * mm tts — text-to-speech via the hub's Kokoro service.
 *
 * Usage:
 *   mm tts "Hello world"                     Write WAV to stdout
 *   mm tts "Hello" --out hello.wav           Write WAV to file
 *   mm tts "Hello" --out hello.mp3           ffmpeg-encode to MP3
 *   mm tts "Hello" --play                    Synthesise and play (afplay)
 *   mm tts - --out out.wav < script.txt      Read text from stdin
 *   mm tts "Hi" --voice af_bella             Pick a voice
 *
 * The hub streams base64-encoded PCM16 chunks at 24kHz via SSE; we
 * concatenate them, prepend a 44-byte WAV header, and emit one
 * complete WAV. MP3 output pipes that WAV through `ffmpeg`.
 */

import { spawn } from 'node:child_process';
import { writeFile, mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadAuth } from '../auth';

const HUB_URL = process.env.MM_HUB_URL || 'https://meta-me.uk';
const TTS_SAMPLE_RATE = 24000;

type TtsFlags = {
	out?: string;
	play?: boolean;
	voice?: string;
	format?: 'wav' | 'mp3';
};

function parseFlags(args: string[]): { text: string; flags: TtsFlags } {
	let text = '';
	const flags: TtsFlags = {};
	for (let i = 0; i < args.length; i++) {
		const a = args[i];
		if (a === '--out') flags.out = args[++i];
		else if (a === '--play') flags.play = true;
		else if (a === '--voice') flags.voice = args[++i];
		else if (a === '--format') flags.format = args[++i] as 'wav' | 'mp3';
		else if (!text) text = a;
	}
	if (!flags.format && flags.out?.endsWith('.mp3')) flags.format = 'mp3';
	if (!flags.format) flags.format = 'wav';
	return { text, flags };
}

export async function ttsDispatch(args: string[]) {
	const { text: positional, flags } = parseFlags(args);
	if (!positional) {
		console.error('Usage: mm tts "<text>" [--out file] [--play] [--voice id] [--format wav|mp3]');
		process.exit(1);
	}

	const text = positional === '-' ? await readStdinText() : positional;
	if (!text.trim()) throw new Error('text input is empty');

	const auth = loadAuth();
	if (!auth) throw new Error('Not authenticated. Run `mm login` first.');

	const res = await fetch(`${HUB_URL}/api/tts/stream`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${auth.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ text, voice: flags.voice || 'af_heart' })
	});
	if (!res.ok || !res.body) {
		const detail = res.body ? await res.text().catch(() => res.statusText) : res.statusText;
		throw new Error(`TTS failed (${res.status}): ${String(detail).slice(0, 200)}`);
	}

	const pcm = await consumePcmSse(res.body);
	if (!pcm.byteLength) throw new Error('TTS returned no audio');
	const wav = wrapWav(pcm, TTS_SAMPLE_RATE);

	const bytes = flags.format === 'mp3' ? await wavToMp3(wav) : wav;

	if (flags.play) {
		const dir = await mkdtemp(join(tmpdir(), 'mm-tts-'));
		const path = join(dir, flags.format === 'mp3' ? 'out.mp3' : 'out.wav');
		await writeFile(path, bytes);
		await playFile(path);
	} else if (flags.out) {
		await writeFile(flags.out, bytes);
		console.error(`wrote ${bytes.byteLength} bytes to ${flags.out}`);
	} else {
		// Default: stream raw bytes to stdout so `mm tts ... | afplay -`
		// or piping into ffmpeg works naturally.
		process.stdout.write(bytes);
	}
}

async function readStdinText(): Promise<string> {
	const chunks: Buffer[] = [];
	for await (const chunk of process.stdin) {
		chunks.push(typeof chunk === 'string' ? Buffer.from(chunk) : (chunk as Buffer));
	}
	return Buffer.concat(chunks).toString('utf8').trim();
}

/** Parse the SSE event stream the hub emits and decode the
 *  base64-encoded PCM chunks into one combined Buffer. */
async function consumePcmSse(body: ReadableStream<Uint8Array>): Promise<Buffer> {
	const reader = body.getReader();
	const decoder = new TextDecoder();
	let buffered = '';
	const out: Buffer[] = [];
	while (true) {
		const { value, done } = await reader.read();
		if (done) break;
		buffered += decoder.decode(value, { stream: true });
		// Split on SSE event boundaries ("\n\n"). Keep the trailing
		// partial in `buffered` for the next read.
		const events = buffered.split('\n\n');
		buffered = events.pop() ?? '';
		for (const raw of events) {
			const dataLine = raw.split('\n').find((l) => l.startsWith('data: '));
			if (!dataLine) continue;
			let evt: { type?: string; audio?: string };
			try {
				evt = JSON.parse(dataLine.slice(6));
			} catch {
				continue;
			}
			if (evt.type === 'chunk' && evt.audio) {
				out.push(Buffer.from(evt.audio, 'base64'));
			}
			if (evt.type === 'done') return Buffer.concat(out);
		}
	}
	return Buffer.concat(out);
}

/** Prepend a 44-byte PCM16 mono RIFF header to raw PCM bytes. */
function wrapWav(pcm: Buffer, sampleRate: number): Buffer {
	const dataLen = pcm.byteLength;
	const buf = Buffer.alloc(44 + dataLen);
	buf.write('RIFF', 0, 'ascii');
	buf.writeUInt32LE(36 + dataLen, 4);
	buf.write('WAVE', 8, 'ascii');
	buf.write('fmt ', 12, 'ascii');
	buf.writeUInt32LE(16, 16); // PCM chunk size
	buf.writeUInt16LE(1, 20); // PCM format
	buf.writeUInt16LE(1, 22); // mono
	buf.writeUInt32LE(sampleRate, 24);
	buf.writeUInt32LE(sampleRate * 2, 28); // byte rate
	buf.writeUInt16LE(2, 32); // block align
	buf.writeUInt16LE(16, 34); // bits per sample
	buf.write('data', 36, 'ascii');
	buf.writeUInt32LE(dataLen, 40);
	pcm.copy(buf, 44);
	return buf;
}

function wavToMp3(wav: Buffer): Promise<Buffer> {
	return new Promise((resolve, reject) => {
		const ff = spawn(
			'ffmpeg',
			['-hide_banner', '-loglevel', 'error', '-i', 'pipe:0', '-f', 'mp3', '-q:a', '4', 'pipe:1'],
			{ stdio: ['pipe', 'pipe', 'pipe'] }
		);
		const out: Buffer[] = [];
		const err: Buffer[] = [];
		ff.stdout.on('data', (d) => out.push(d));
		ff.stderr.on('data', (d) => err.push(d));
		ff.on('error', (e) => reject(new Error(`ffmpeg spawn failed: ${e.message}`)));
		ff.on('close', (code) =>
			code === 0
				? resolve(Buffer.concat(out))
				: reject(new Error(`ffmpeg exited ${code}: ${Buffer.concat(err).toString('utf8').trim()}`))
		);
		ff.stdin.end(wav);
	});
}

function playFile(path: string): Promise<void> {
	const player =
		process.platform === 'darwin' ? 'afplay' : process.platform === 'linux' ? 'aplay' : null;
	if (!player) {
		console.error(`No supported player for platform ${process.platform}; wrote to ${path}`);
		return Promise.resolve();
	}
	return new Promise((resolve, reject) => {
		const p = spawn(player, [path], { stdio: 'inherit' });
		p.on('error', (e) => reject(new Error(`${player} not found: ${e.message}`)));
		p.on('close', (code) =>
			code === 0 ? resolve() : reject(new Error(`${player} exited ${code}`))
		);
	});
}
