/**
 * ProtectDisConnectMessage command parameters
 *
 * @export
 * @interface ProtectDisConnectMessageCommandParams
 */
export interface ProtectDisConnectMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectDisConnectMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectDisConnectMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof ProtectDisConnectMessageCommandParams
     */
    destinationId: number;

    /**
     * Device ID of the panel matrix
     * + Range [0 - 65535]
     * @type {number}
     * @memberof ProtectDisConnectMessageCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectDisConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectDisConnectMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectDisConnectMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
