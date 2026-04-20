import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * SingleDestinationAssociationNamesRequestMessage command options

 * @export
 * @interface SingleDestinationAssociationNamesRequestMessageCommandOptions
 */
export interface SingleDestinationAssociationNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {LengthOfNamesRequired}
     * @memberof LengthOfNamesRequiredCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing some extra functionality on the SingleDestinationAssociationNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {SingleDestinationAssociationNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: SingleDestinationAssociationNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the LengthOfNamesRequired
     *
     *
     * @param {LengthOfNamesRequired} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired command textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
