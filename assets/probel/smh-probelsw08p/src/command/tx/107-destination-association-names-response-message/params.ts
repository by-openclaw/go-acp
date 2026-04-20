/**
 * Destination Association Names Response Message Command Input Paramameters
 *
 * @export
 * @interface DestinationAssociationNamesResponseCommandParams
 */
export interface DestinationAssociationNamesResponseCommandParams {
    /**
     *
     * Matrix Number (0 – 19)
     * @type {number}
     * @memberof DestinationAssociationNamesResponseCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number, only applicable to Source Name type (0 -15)
     * @type {number}
     * @memberof DestinationAssociationNamesResponseCommandParams
     */
    levelId: number;

    /**
     *
     * First Destination number
     * @type {number}
     * @memberof DestinationAssociationNamesResponseCommandParams
     */
    firstDestinationAssociationId: number;

    /**
     *
     * Number of Destination Association Names to follow (in this message, maximum of
     * + 32 for 4 char names
     * + 16 for 8 char names
     * + 10 for 12 char names
     * @type {number}
     * @memberof DestinationAssociationNamesResponseCommandParams
     */
    numberOfDestinationAssociationNamesToFollow: number;

    /**
     * the labels size contains in the buffer depend of :
     * - 'lenOfNames' propertie :
     *  + if lenOfNames = 00 then max buffer is 04 char x 32 labels
     *  + if lenOfNames = 01 then max buffer is 08 char x 16 labels
     *  + if lenOfNames = 02 then max buffer is 12 char x 10 labels
     *
     * @type {string[]}
     * @memberof DestinationAssociationNamesResponseCommandParams
     */
    destinationAssociationNameItems: string[];
}

/**
 * Utility class providing extra functionalities on the SourceNamesResponse command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {DestinationAssociationNamesResponseCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    // TODO: add the list of destinationAssociationNameItems
    static toString(params: DestinationAssociationNamesResponseCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, First Destination number :${params.firstDestinationAssociationId}, Number Of Destination Association Names To Follow: ${params.numberOfDestinationAssociationNamesToFollow}, destinationAssociationNameItems: []`;
    }
}
