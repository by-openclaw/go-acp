export interface NetworkServerOptions {
    maxConnections: number;
    connectionKeepAlive: boolean;
    connectionKeepAliveTimeout: number;
}

/**
 * Defines the server options.
 *
 * @export
 * @interface NetworkSocketOptions
 */
export interface NetworkSocketOptions {
    /**
     * No operation property
     * @optional
     */
    noop?: number;
}

/**
 * Defines the default server options.
 *
 * @export
 */
export const DEFAULT_SERVER_OPTIONS: NetworkServerOptions = {
    maxConnections: 2, // Rejectconnections when the server's connection count gets high.
    connectionKeepAlive: true,
    connectionKeepAliveTimeout: 10000 // 10 sec
};
