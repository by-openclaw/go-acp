/**
 * CrossPointConnectOnGoSalvoGroupMessage command items
 *
 * @export
 * @interface CrossPointConnectOnGoSalvoGroupMessageCommandItems
 */
export interface CrossPointConnectOnGoSalvoGroupMessageCommandItems {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandItems
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandItems
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandItems
     */
    destinationId: number;

    /**
     * Source ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandItems
     */
    sourceId: number;

    /**
     * Salvo number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 127]
     * @type {number}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandItems
     */
    salvoId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointConnectOnGoGroupSalvo command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandItemsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandItems} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandItemsUtility
     */
    static toString(params: CrossPointConnectOnGoSalvoGroupMessageCommandItems): string {
        return `${this.name}: Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source:${params.sourceId}, Salvo:${params.salvoId}`;
    }
}
