/**
 * SingleDestinationAssociationNamesRequestMessage command parameters
 *
 * @export
 * @interface SingleDestinationAssociationNamesRequestMessageCommandParams
 */
export interface SingleDestinationAssociationNamesRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof SingleDestinationAssociationNamesRequestMessageCommandParams
     */
    matrixId: number;

    /**
     * Destination number
     * + Rang[0-65535]
     *
     * @type {number}
     * @memberof SingleDestinationAssociationNamesRequestMessageCommandParams
     */
    destinationId: number;
}

/**
 * Utility class providing extra functionalities on the SingleDestinationAssociationNamesRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {SingleDestinationAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: SingleDestinationAssociationNamesRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Destination:${params.destinationId}`;
    }
}
