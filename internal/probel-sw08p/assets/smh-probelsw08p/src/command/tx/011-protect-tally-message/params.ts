/**
 * ProtectTallyMessage command parameters
 *
 * @export
 * @interface ProtectTallyCommandParams
 */
export interface ProtectTallyCommandParams {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof ProtectTallyCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof ProtectTallyCommandParams
     */
    levelId: number;

    /**
     *
     * Destination number
     * @type {number}
     * @memberof ProtectTallyCommandParams
     */
    destinationId: number;

    /**
     *
     * Device number
     * Device (0- 1023 Devices)
     * @type {number}
     * @memberof ProtectTallyCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectTallyMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectTallyCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectTallyCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Device:${params.deviceId}`;
    }
}
