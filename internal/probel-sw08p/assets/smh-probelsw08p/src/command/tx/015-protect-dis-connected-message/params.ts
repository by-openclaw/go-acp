/**
 * ProtectDis-ConnectMessage command parameters
 *
 * @export
 * @interface ProtectDiscConnectedCommandParams
 */
export interface ProtectDiscConnectedCommandParams {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof ProtectDiscConnectedCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof ProtectDiscConnectedCommandParams
     */
    levelId: number;

    /**
     *
     * Destination number
     * @type {number}
     * @memberof ProtectDiscConnectedCommandParams
     */
    destinationId: number;

    /**
     *
     * Device number
     * Device (0- 1023 Devices)
     * @type {number}
     * @memberof ProtectDiscConnectedCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectDis-ConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectDiscConnectedCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectDiscConnectedCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
