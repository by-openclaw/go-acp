import { EventEmitter } from 'events';
import { AddressInfo } from 'net';
import * as net from 'net';

import { CommandParserService } from '../command-parser';
import { LoggingService } from '../common/logging/logging.service';
import { Maybe } from '../common/util/type';
import { JsonUtility } from '../common/utility/json.utility';
import { NetworkServerOptions } from './network-server.options';
import { NetworkServerStatus } from './network-server.status';

/**
 * This class is used to create a TCP or IPC SMH Inbound Connector - Server component.
 *
 * @export
 * @class NetworkServer
 * @extends {EventEmitter}
 */
export class NetworkServer extends EventEmitter {
    private _status: NetworkServerStatus;
    private _server!: net.Server;
    private _clients: Map<string, net.Socket>;

    // this._server.getMaxListeners
    // this._server.getConnections
    // this._server.listenerCount
    // this._server.listeners
    // this._server.setMaxListeners
    /**
     * Gets the Server Network status.
     *
     * @type {NetworkServerStatus}
     * @memberof NetworkServer
     */
    get status(): NetworkServerStatus {
        return this._status;
    }

    /**
     * Gets the server bound address, the address family name and port of the socket as reported by the operating system
     * { port: 12346, family: 'IPv4', address: '127.0.0.1' }
     *
     * @readonly
     * @type {Maybe<AddressInfo>}
     * @memberof NetworkServer
     */
    get boundAddress(): Maybe<AddressInfo> {
        return this._server?.address() as AddressInfo;
    }

    /**
     * Indicates whether or not the server is listening for connections.
     *
     * @readonly
     * @type {boolean}
     * @memberof NetworkServer
     */
    get isListening(): boolean {
        return this._server?.listening;
    }

    /**
     * Asynchronously get the number of concurrent connections on the server. Works when sockets were sent to forks.
     *
     * @readonly
     * @type {Promise<number>}
     * @memberof NetworkServer
     */
    get connectionsAsync(): Promise<number> {
        return new Promise((resolve: any, reject: any) => {
            this._server.getConnections((error?: Error | null, count?: number) => {
                if (error) {
                    this._loggingService.error(() => `${NetworkServer.name}|'connectionsAsync'`);
                }
                resolve(error ? 0 : count);
            });
        });
    }

    /**
     * Sets the server status and emits 'status-changed' event.
     *
     * @memberof NetworkServer
     */
    set status(value: NetworkServerStatus) {
        if (value !== this._status) {
            const statusPayload = NetworkServer.createStatusEventPayload(this.status, value);
            this.emit('status-changed', statusPayload);
            this._status = value;
        }
    }

    /**
     * Creates an instance of NetworkServer.
     *
     * @param {LoggingService} _loggingService
     * @param {CommandParserService} _dataLayerDecoderService
     * @param {NetworkServerOptions} _options
     * @memberof NetworkServer
     */
    constructor(
        private _loggingService: LoggingService,
        private _dataLayerDecoderService: CommandParserService,
        private _options: NetworkServerOptions
    ) {
        super();
        this._loggingService.trace(() => `${NetworkServer.name} is created with\n`, JsonUtility.stringify(_options));
        this._status = 'idle';
        this._clients = new Map<string, net.Socket>();
    }

    /**
     * Creates teh status event payload.
     *
     * @private
     * @static
     * @param {NetworkServerStatus} from
     * @param {NetworkServerStatus} to
     * @returns {object}
     * @memberof NetworkServer
     */
    private static createStatusEventPayload(from: NetworkServerStatus, to: NetworkServerStatus): object {
        return { from, to };
    }

    /**
     * Gets the server bound address description : 'address:port:family'
     *
     * @private
     * @static
     * @param {net.Socket} socket
     * @returns {string}
     * @memberof NetworkServer
     */
    private static getRemoteBoundAddressDescription(socket: net.Socket): string {
        return `${socket.remoteAddress}:${socket.remotePort}:${socket.remoteFamily}`;
    }

    /**
     * Starts asynchronously the server.
     *
     * @param {string} address - address the server will bind to
     * @param {number} port - port the server will listen to
     * @returns {Promise<void>}
     * @memberof NetworkServer
     * @async
     */
    startAsync(address: string, port: number): Promise<void> {
        // Guard
        if (this.status !== 'idle') {
            const error = new Error(
                `Server must be in idle status to be started ! (server status is currently '${this.status}')`
            );
            this._loggingService.error(() => `${NetworkServer.name}|${this.startAsync.name}`, error);
            throw error;
        }

        this._loggingService.trace(
            () => `${NetworkServer.name}|${this.startAsync.name} with [address:${address}], [port:${port}]...`
        );
        this.status = 'starting';

        this._server = net.createServer((socket: net.Socket) => {
            this._server.maxConnections = this._options.maxConnections;
            const name = socket.remoteAddress + ':' + socket.remotePort;
            socket.write('Welcome ' + name + '\n');

            socket.setKeepAlive(true, 600); // 1 min = 60000 milliseconds.

            this.addClient(socket);
        });

        // Server starts listening on events
        this._server

            // Emitted when the server closes.
            // If connections exist, this event is not emitted until all connections are ended.
            .on('close', (e: Error) => {
                this._loggingService.debug(
                    () => `${NetworkServer.name}|${this.startAsync.name} net.Server event received [event:'close']...`
                );
            })

            // Emitted when a new connection is made.
            // socket is an instance of net.Socket.
            .on('connection', (socket: net.Socket) => {
                this._loggingService.debug(
                    () =>
                        `${NetworkServer.name}|${this.startAsync.name} net.Server event received [event:'connection']\n`,
                    NetworkServer.getRemoteBoundAddressDescription(socket)
                );
            })

            // Emitted when a new connection is made. socket is an instance of net.Socket.
            .on('error', (error: Error) => {
                this._loggingService.error(
                    () =>
                        `${NetworkServer.name}|${this.startAsync.name} net.Server event received [event:'error'] with \n`,
                    error
                );
            })

            // Emitted when the server has been bound after calling server.listen().
            .on('listening', () => {
                this._loggingService.debug(
                    () =>
                        `${NetworkServer.name}|${this.startAsync.name} net.Server event received [event:'listening']...`
                );
                this.status = 'listening';
                // this.emit(S101ServerEventNames.LISTENING);
                // this.status = S101ServerStatuses.LISTENING;
            });

        // socket.end

        // Start the server listening for connections.
        // A net.Server can be a TCP or an IPC server depending on what it listens to.
        return new Promise((resolve, reject) => {
            this._server.listen(port, address, () => {
                this._loggingService.debug(
                    () => `${NetworkServer.name}|${this.startAsync.name} is listening on ${this.boundAddress}`
                );
                this.status = 'listening';
                return resolve();
            });
        });
    }

    /**
     * Stop asynchronously the server.
     *
     * @returns {Promise<void>}
     * @memberof NetworkServer
     * @async
     */
    stopAsync(): Promise<void> {
        // Guard
        // @TODO: use never
        switch (this.status) {
            case 'idle':
            case 'starting':
            case 'stopping':
                const error = new Error(
                    `Server must be started to be started ! (server status is currently '${this.status}')`
                );
                this._loggingService.error(() => `${NetworkServer.name}|${this.stopAsync.name}`, error);
                throw error;
                break;

            case 'stopped':
            case 'error':
            case 'listening':
            default:
                break;
        }
        this._loggingService.trace(() => `${NetworkServer.name}|${this.stopAsync.name}...`);

        this.status = 'stopping';

        // Stops the server from accepting new connections and keeps existing connections.
        // This function is asynchronous, the server is finally closed when all connections are ended and the server emits a 'close' event.
        // The optional callback will be called once the 'close' event occurs.
        // Unlike that event, it will be called with an Error as its only argument if the server was not open when it was closed.
        return new Promise((resolve: any, reject: any) => {
            this._server.close((error: Maybe<Error>) => {
                if (error) {
                    this._loggingService.error(() => `${NetworkServer.name}|${this.stopAsync.name}...`);
                    this.status = 'stopped';
                    return reject(error);
                }
                this.status = 'stopped';
                return resolve();
            });
            this._clients.forEach((s: net.Socket) => s.end());
        });
    }

    /**
     * Add a new connected socket client.
     *
     * @private
     * @param {net.Socket} socket - connected socket client
     * @memberof NetworkServer
     */
    private addClient(socket: net.Socket): void {
        this._loggingService.trace(
            () =>
                `${NetworkServer.name}|${
                    this.addClient.name
                } ${`Server: [${this.boundAddress?.address}:${this.boundAddress?.port}:${this.boundAddress?.family}] <--> [${socket.remoteAddress}:${socket.remotePort}:${socket.remoteFamily}]`}`
        );

        // Set initialDelay (in milliseconds) to set the delay between the last data packet received and the first keepalive probe. Setting 0 for initialDelay will leave the value unchanged from the default (or previous) setting.
        socket.setKeepAlive(this._options.connectionKeepAlive, this._options.connectionKeepAliveTimeout);
        // Emitted once the socket is fully closed. The argument hadError is a boolean which says if the socket was closed due to a transmission error.
        socket.on('close', () => {
            this.removeClient(socket);
            socket.destroy();
        });

        socket.on('data', (data: any) => {
            this._loggingService.debug(
                () =>
                    `${NetworkServer.name}|${
                        this.addClient.name
                    } net.Server client ${NetworkServer.getRemoteBoundAddressDescription(socket)} receive data`,
                data
            );

            //            this._dataLayerDecoder.decode(data);
        });

        // Emitted when a socket is ready to be used.
        socket.on('ready', () => {
            this._loggingService.trace(() => `${NetworkServer.name}|${this.addClient.name} READY...`);
        });

        const clientBoundAddress = NetworkServer.getRemoteBoundAddressDescription(socket);
        this._clients.set(clientBoundAddress, socket);
        this._loggingService.trace(
            () =>
                `${NetworkServer.name}|${this.addClient.name} Client ${clientBoundAddress} added to the map #count=${this._clients.size}... `
        );
    }

    /**
     * Remove a connected socket client.
     *
     * @private
     * @param {net.Socket} socket - socket connected client
     * @memberof NetworkServer
     */
    private removeClient(socket: net.Socket): void {
        this._loggingService.trace(
            () =>
                `${NetworkServer.name}|${
                    this.removeClient.name
                } ${`Server: [${this.boundAddress?.address}:${this.boundAddress?.port}:${this.boundAddress?.family}] <--> [${socket.remoteAddress}:${socket.remotePort}:${socket.remoteFamily}]`}`
        );

        const clientBoundAddress = NetworkServer.getRemoteBoundAddressDescription(socket);
        this._clients.delete(clientBoundAddress);
        this._loggingService.trace(
            () =>
                `${NetworkServer.name}|${this.removeClient.name} Client ${clientBoundAddress} removed from the map #count=${this._clients.size}... `
        );
    }
}
