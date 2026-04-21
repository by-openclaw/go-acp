/**
 * ProtectConnected command parameters
 *
 * @export
 * @interface ProtectConnectedCommandParams
 */
export interface ProtectConnectedCommandParams {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof ProtectConnectedCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof ProtectConnectedCommandParams
     */
    levelId: number;

    /**
     *
     * Destination number
     * @type {number}
     * @memberof ProtectConnectedCommandParams
     */
    destinationId: number;

    /**
     *
     * Device number
     * Device (0- 1023 Devices)
     * @type {number}
     * @memberof ProtectConnectedCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectConnectedMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectConnectedCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectConnectedCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
