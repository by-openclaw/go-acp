/**
 * Maintenance Message command parameters
 *
 * @export
 * @interface MaintenanceMessageCommandParams
 */
export interface MaintenanceMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 19]
     * + If CLEAR_PROTECTS then Matrix Id could be = [255] to Clear all matrices
     *
     * @type {number}
     * @memberof MaintenanceMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 15]
     * + If CLEAR_PROTECTS then Level Id could be = [255] to Clear all levels
     *
     * @type {number}
     * @memberof MaintenanceMessageCommandParams
     */
    levelId: number;
}

/**
 * Utility class providing extra functionalities on the MaintenanceMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @param {MaintenanceMessageCommandParams} params the command parameters
     * @returns {string} the commadn parameter textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: MaintenanceMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}`;
    }
}
