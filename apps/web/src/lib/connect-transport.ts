import 'server-only';
import { createClient, type Client, type Transport } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-node';
import type { DescService } from '@bufbuild/protobuf';

const transports = new Map<string, Transport>();

export function getInternalConnectTransport(baseUrl: string, defaultTimeoutMs: number): Transport {
  const key = `${baseUrl}|${defaultTimeoutMs}`;
  let transport = transports.get(key);
  if (!transport) {
    transport = createConnectTransport({
      baseUrl,
      httpVersion: '1.1',
      useBinaryFormat: true,
      defaultTimeoutMs
    });
    transports.set(key, transport);
  }
  return transport;
}

export function getInternalConnectClient<T extends DescService>(
  service: T,
  baseUrl: string,
  defaultTimeoutMs: number
): Client<T> {
  return createClient(service, getInternalConnectTransport(baseUrl, defaultTimeoutMs));
}
