/**
 * CrossPointConnectMessage command parameters
 *
 * @export
 * @interface CrossPointConnectMessageCommandParams
 */
export interface CrossPointConnectMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointConnectMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointConnectMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof CrossPointConnectMessageCommandParams
     */
    destinationId: number;

    /**
     * Source ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof CrossPointConnectMessageCommandParams
     */
    sourceId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointConnectMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointConnectMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source:${params.sourceId}`;
    }
}
