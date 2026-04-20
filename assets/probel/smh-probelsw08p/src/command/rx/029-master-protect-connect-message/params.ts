/**
 * MasterProtectConnectMessage command parameters
 * @export
 * @interface MasterProtectConnectMessageCommandParams
 */
export interface MasterProtectConnectMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof MasterProtectConnectMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof MasterProtectConnectMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof MasterProtectConnectMessageCommandParams
     */
    destinationId: number;

    /**
     * Device ID of the panel matrix
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof MasterProtectConnectMessageCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the MasterProtectConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {MasterProtectConnectMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: MasterProtectConnectMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
