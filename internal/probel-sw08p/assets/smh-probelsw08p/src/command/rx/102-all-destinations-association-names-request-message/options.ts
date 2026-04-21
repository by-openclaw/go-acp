import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * AllDestinationsAssociationNamesRequestMessage command options

 * @export
 * @interface AllDestinationsAssociationNamesRequestMessageCommandOptions
 */
export interface AllDestinationsAssociationNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {LengthOfNamesRequired}
     * @memberof LengthOfNamesRequiredCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing some extra functionality on the AllDestinationsAssociationNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {AllDestinationsAssociationNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: AllDestinationsAssociationNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the LengthOfNamesRequired
     *
     *
     * @param {LengthOfNamesRequired} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
