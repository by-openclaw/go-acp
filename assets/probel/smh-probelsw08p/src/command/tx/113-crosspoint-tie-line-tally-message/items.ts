/**
 * CrossPoint Tie Line Tally Message command Items
 *
 * @export
 * @interface CrossPointTieLineTallyCommandItems
 */
export interface CrossPointTieLineTallyCommandItems {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandItems
     */
    sourceMatrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandItems
     */
    sourceLevel: number;

    /**
     *
     * Source number
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandItems
     */
    sourceId: number;
}

/**
 * Utility class providing extra functionalities on the CrosspointTieLineTallyMessage command parameters
 *
 * @export
 * @class CommandItemsUtility
 */
export class CommandItemsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointTieLineTallyCommandItems} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandItemsUtility
     */
    static toString(params: CrossPointTieLineTallyCommandItems): string {
        // TODO : Add deviceNumberProtectDataItems[]
        return `sourceMatrixId:${params.sourceMatrixId}, sourceLevel: ${params.sourceLevel} sourceId:${params.sourceId})}`;
    }
}
