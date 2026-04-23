import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointSalvoGroupInterrogateMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Salvo Group Interrogate Message command
 *
 * This message is issued by the remote device to request the status of the GROUP SALVOS.
 * The controller will respond with a CROSSPOINT GROUP SALVO TALLY message (Command Byte 125).
 * N.B.: The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
 *
 * Command issued by the remote device
 * @export
 * @class CrossPointSalvoGroupInterrogateMessageCommand
 * @extends {CommandBase<CrossPointSalvoGroupInterrogateMessageCommandParams>}
 */
export class CrossPointSalvoGroupInterrogateMessageCommand extends CommandBase<
    CrossPointSalvoGroupInterrogateMessageCommandParams,
    any
> {
    /**
     * Creates an instance of CrossPointSalvoGroupInterrogateMessageCommand
     *
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    constructor(params: CrossPointSalvoGroupInterrogateMessageCommandParams) {
        super(CrossPointSalvoGroupInterrogateMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (isConnectIndexId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    private static isExtended(params: CrossPointSalvoGroupInterrogateMessageCommandParams): boolean {
        // two bytes
        return params.connectIndexId > 255;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    private static getCommandId(params: CrossPointSalvoGroupInterrogateMessageCommandParams): CommandIdentifier {
        return CrossPointSalvoGroupInterrogateMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE
            : CommandIdentifiers.RX.GENERAL.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return CrossPointSalvoGroupInterrogateMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointSalvoGroupInterrogateMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    private validateParams(params: CrossPointSalvoGroupInterrogateMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds general command
     * Returns Probel SW-P-08 - General CrossPoint Salvo Group Interrogate Message CMD_124_0X7c
     *
     * + This message is issued by the remote device to request the status of the GROUP SALVOS.
     * + The controller will respond with a CROSSPOINT GROUP SALVO TALLY message (Command Byte 125).
     *
     * + N.B.: The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message | Command Byte | 124 - 0x7c                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | salvo number | Salvo group number                                                                                                                 |
     * | Byte 2  | connect index| this specifies the index into the SALVO GROUP specified in byte1. This command is called recursively from connect index 0          |
     * |         |              | , until no crosspoint data in the specified group is left.                                                                         |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.salvoId)
            .writeUInt8(this.params.connectIndexId)
            .toBuffer();
    }

    /**
     * Build the extended command
     * Returns Probel SW-P-08 - Extended CrossPoint Salvo Group Interrogate Message CMD_248_0Xf8
     *
     * + This message is issued by the remote device to request the status of the GROUP SALVOS.
     * + The controller will respond with an EXTENDED CROSSPOINT GROUP SALVO TALLY message (Command Byte 253).
     *
     * + N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.

     * | Message |  Command Byte   | 252 - 0xfc                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Salvo num       | Holds the Salvo group number to configure                                                                                       |
     * |         | Bit[7]          | 0                                                                                                                               |
     * |         | Bit[0-6]        | Salvo number 0-127                                                                                                              |
     * |         |                 | Destination and source will always overwrite previous data.                                                                     |
     * | Byte 2  | Connect mult    | Connect Index number DIV 256                                                                                                    |
     * | Byte 3  | Connect Index   | Destination number MOD 256                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointSalvoGroupInterrogateMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.salvoId)
            .writeUInt8(Math.floor(this.params.connectIndexId / 256))
            .writeUInt8(this.params.connectIndexId % 256)
            .toBuffer();
    }
}
