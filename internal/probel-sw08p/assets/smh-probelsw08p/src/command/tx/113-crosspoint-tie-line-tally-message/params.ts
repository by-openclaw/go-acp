import { CrossPointTieLineTallyCommandItems } from './items';

/**
 * CrossPoint Tie Line Tally Message command params
 * Comments: The Tie Line Tally command contains a Source Matrix, Source Level and Source Number for every Destination in the specified Destination Association.
 * @export
 * @interface CrossPointTieLineTallyCommandParams
 */
export interface CrossPointTieLineTallyCommandParams {
    /**
     *
     * Destination Matrix Number (0-19)
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandParams
     */
    destinationMatrixId: number;

    /**
     *
     * Destination Association Number
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandParams
     */
    destinationAssociation: number;

    /**
     *
     * numberOfSourcesReturned number
     * @type {number}
     * @memberof CrossPointTieLineTallyCommandItems
     */
    numberOfSourcesReturned: number;

    /**
     *
     *
     * @type {CrossPointTieLineTallyCommandItems[]}
     * @memberof CrossPointTieLineTallyCommandParams
     */
    sourceItems: CrossPointTieLineTallyCommandItems[];
}

/**
 * Utility class providing extra functionalities on the CrossPointTieLineTallyMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointTieLineTallyCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointTieLineTallyCommandParams): string {
        // TODO : Add sourceItems[]
        return `Destination MatrixId:${params.destinationMatrixId}, Destination Association:${params.destinationAssociation}, Number Of Protect Tallies:${params.numberOfSourcesReturned}, sourceItems[]}`;
    }
}
