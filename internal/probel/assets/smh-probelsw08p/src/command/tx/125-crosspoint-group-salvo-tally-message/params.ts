/**
 * @export
 * @interface CrossPointGroupSalvoTallyCommandParams
 */
export interface CrossPointGroupSalvoTallyCommandParams {
    /**
     *
     * Matrix number
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    levelId: number;

    /**
     *
     * Destination number
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    destinationId: number;

    /**
     *
     * Source number
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    sourceId: number;

    /**
     *
     * Salvo Group number
     * as defined in 3.1.29
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    salvoId: number;

    /**
     *
     * Salvo Group number
     * this specifies the index into the SALVO GROUP specified in byte5.
     * This command is called recursively from connect index 0, until no crosspoint data in the specified group is left.
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandParams
     */
    connectIndex: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointGroupSalvoTallyMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointGroupSalvoTallyCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointGroupSalvoTallyCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source Id:${params.sourceId}, Salvo Id:${params.salvoId}, Connect Index:${params.connectIndex} `;
    }
}
