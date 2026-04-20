/**
 * ProtectConnectMessage command parameters
 *
 * @export
 * @interface ProtectConnectMessageCommandParams
 */
export interface ProtectConnectMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectConnectMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectConnectMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof ProtectConnectMessageCommandParams
     */
    destinationId: number;

    /**
     * Device ID of the panel matrix
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof ProtectConnectMessageCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectConnectMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectConnectMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
