import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandOptionsUtility, UpdateRenameRequestCommandOptions } from './options';
import { CommandParamsUtility, UpdateRenameRequestCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Implements the Update Rename Request command Message
 *
 * + This message is issued by a remote device to remotely update a name in the system database of the controller. There is no response to this command.
 * + NOTE: Using this command on an Aurora or similar controller will result in a database mismatch when the system configuration editor is next connected online.
 * + This will mean that before edits are done using the system editor the database from the controller will need to be uploaded first otherwise any remote name changes will be lost.
 *
 * Command issued by the remote device
 *
 * @export
 * @class UpdateNameRequestCommand
 * @extends {CommandBase<UpdateRenameNameRequestCommandParams>}
 */
export class UpdateRenameRequestCommand extends MetaCommandBase<
    UpdateRenameRequestCommandParams,
    UpdateRenameRequestCommandOptions
> {
    /**
     * Creates an instance of UpdateNameRequestCommand.
     * @param {UpdateNameRequestCommandParams} params the command parameters
     * @param {UpdateRenameRequestCommandOptions} _options the command options
     * @memberof UpdateRenameRequestCommand
     */
    constructor(params: UpdateRenameRequestCommandParams, options: UpdateRenameRequestCommandOptions) {
        super(CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE, params, options);
        this.validateParams(params, options);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof UpdateRenameRequestCommand
     */
    toLogDescription(): string {
        // TODO: Add List of Name chars[]
        return CommandOptionsUtility.isSourceName(this.options)
            ? `Source Name - ${this.name}: ${CommandParamsUtility.toString(
                  this.params,
                  true
              )}, ${CommandOptionsUtility.toString(this.options)}, Number of Names to follow : ${
                  this.params.nameCharsItems.length
              }`
            : `General - ${this.name}: ${CommandParamsUtility.toString(
                  this.params,
                  false
              )},${CommandOptionsUtility.toString(this.options)}, Number of Names to follow : ${
                  this.params.nameCharsItems.length
              }`;
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof UpdateRenameRequestCommand
     */
    protected buildData(): Buffer[] {
        // Padding all the nameCharsItems with Length of Names Required
        const nameCharsItemsPadding = new Array<string>();
        for (let nbrPadding = 0; nbrPadding < this.params.nameCharsItems.length; nbrPadding++) {
            nameCharsItemsPadding.push(
                BufferUtility.decPadStringEnd(
                    this.params.nameCharsItems[nbrPadding],
                    this.options.lengthOfNames.byteLength,
                    // TODO: Add in the admin settings and could be changed by the admin at anytime.
                    '-'
                )
            );
        }

        // Creates an array of nameCharsItemsPadding split into groups the length of the byteMaximumNumberOfNames.
        const nameCharsItemsChunck = BufferUtility.getChunkOfStringArray(
            nameCharsItemsPadding,
            this.options.lengthOfNames.byteMaximumNumberOfNames
        );

        // Gets the first time the First name number
        let firstNameOffsetId = this.params.firstNameNumber;

        // Gets the list of command to send
        const buildDataExtendArray = new Array<Buffer>();

        // Generate the List of Commands to send
        for (let nbrCmdToSend = 0; nbrCmdToSend < nameCharsItemsChunck.length; nbrCmdToSend++) {
            if (CommandOptionsUtility.isSourceName(this.options)) {
                // Generate Source Name command
                // Add the command buffer to the array
                buildDataExtendArray.push(
                    this.buildDataExtended(
                        this.params,
                        this.options,
                        nameCharsItemsChunck[nbrCmdToSend],
                        firstNameOffsetId
                    )
                );
            } else {
                // Generate others nameOfType
                // Add the command buffer to the array
                buildDataExtendArray.push(
                    this.buildDataNormal(
                        this.params,
                        this.options,
                        nameCharsItemsChunck[nbrCmdToSend],
                        firstNameOffsetId
                    )
                );
            }

            // Add the new First Name Number update for next command
            firstNameOffsetId += nbrCmdToSend - 1 + (nbrCmdToSend + 1) * nameCharsItemsChunck[nbrCmdToSend].length;
        }
        return buildDataExtendArray;
    }

    /**
     * Builds the general command
     * Returns Probel SW-P-08 - General Update Name Request Message CMD_117_0X75
     *
     * + This message is issued by a remote device to remotely update a name in the system database of the controller. There is no response to this command.
     *
     * + NOTE: Using this command on an Aurora or similar controller will result in a database mismatch when the system configuration editor is next connected online.
     *
     * + This will mean that before edits are done using the system editor the database from the controller will need to be uploaded first otherwise any remote name changes will be lost.
     *
     * | Message                        | Command Byte | 117-0x75 (options.nameOfType != NameType.SOURCE_NAME)                                                        |
     * |--------------------------------|--------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format | Notes                                                                                                        |
     * | Byte 1                         | Name Type    |                                                                                                              |
     * |                                | 0            | Source Name                                                                                                  |
     * |                                | 1            | Source Association Name                                                                                      |
     * |                                | 2            | Destination Association Name                                                                                 |
     * |                                | 3            | UMD Label                                                                                                    |
     * | Byte 2                         | Name Length  | Length of Names Required                                                                                     |
     * |                                | 0            | 4 Character                                                                                                  |
     * |                                | 1            | 8 Character                                                                                                  |
     * |                                | 2            | 12 Character                                                                                                 |
     * |                                | 3            | 16 Character                                                                                                 |
     * | Byte 3                         | Matrix number| Matrix Number (0 – 19)                                                                                       |
     * | Byte 4                         | 1st Name mult| First name number multiplier DIV 256                                                                         |
     * | Byte 5                         | 1st Name num | First name number MOD 256                                                                                    |
     * | Byte 6                         | N            | N = Number of names to follow ...                                                                            |
     * | Bytes 7 to n                   | Name Chars   | See notes below                                                                                              |
     *
     * @protected
     * @param {UpdateRenameRequestCommandParams} params
     * @param {UpdateRenameRequestCommandOptions} options
     * @param {string[]} items
     * @param {number} firstNameOffsetId
     * @returns {Buffer} the command message
     * @memberof UpdateRenameRequestCommand
     */
    protected buildDataNormal(
        params: UpdateRenameRequestCommandParams,
        options: UpdateRenameRequestCommandOptions,
        items: string[],
        firstNameOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfNames.byteLength * items.length + 7;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE.id)
            .writeUInt8(options.nameOfType) // Byte 1 : Name type
            .writeUInt8(options.lengthOfNames.type) // Byte 2 : Length of Names Required
            .writeUInt8(params.matrixId) // Byte 3 : Matrix Id
            .writeUInt8(Math.floor(firstNameOffsetId / 256)) // Byte 4 : First name number multiplier DIV 256
            .writeUInt8(firstNameOffsetId % 256) // Byte 5 : First name number MOD 256
            .writeUInt8(items.length); // Byte 6 : Number of names to follow ...
        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            buffer.writeString(items[itemIndex]);
        }

        return buffer.toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - General Update Name Request Message CMD_117_0X75
     *
     * + This message is issued by a remote device to remotely update a name in the system database of the controller. There is no response to this command.
     * + NOTE: Using this command on an Aurora or similar controller will result in a database mismatch when the system configuration editor is next connected online.
     * + This will mean that before edits are done using the system editor the database from the controller will need to be uploaded first otherwise any remote name changes will be lost.
     *
     * | Message                        | Command Byte | 117-0x75 ( options.nameOfType === NameType.SOURCE_NAME)                                                      |
     * |--------------------------------|--------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format | Notes                                                                                                        |
     * | Byte 1                         | Name Type    |                                                                                                              |
     * |                                | 0            | Source Name                                                                                                  |
     * |                                | 1            | Source Association Name                                                                                      |
     * |                                | 2            | Destination Association Name                                                                                 |
     * |                                | 3            | UMD Label                                                                                                    |
     * | Byte 2                         | Name Length  | Length of Names Required                                                                                     |
     * |                                | 0            | 4 Character                                                                                                  |
     * |                                | 1            | 8 Character                                                                                                  |
     * |                                | 2            | 12 Character                                                                                                 |
     * |                                | 3            | 16 Character                                                                                                 |
     * | Byte 3                         | Matrix number| Matrix Number (0 – 19)                                                                                       |
     * | Byte 4                         | Level number | Level number, only applicable to Source Name type (0 -15)                                                    |
     * | Byte 5                         | 1st Name mult| First name number multiplier DIV 256                                                                         |
     * | Byte 6                         | 1st Name num | First name number MOD 256                                                                                    |
     * | Byte 7                         | N            | N = Number of names to follow ...                                                                            |
     * | Bytes 8 to n                   | Name Chars   | See notes below                                                                                              |
     *
     * @protected
     * @param {UpdateRenameRequestCommandParams} params
     * @param {UpdateRenameRequestCommandOptions} options
     * @param {string[]} items
     * @param {number} firstNameOffsetId
     * @returns {Buffer} the command message
     * @memberof UpdateRenameRequestCommand
     */
    protected buildDataExtended(
        params: UpdateRenameRequestCommandParams,
        options: UpdateRenameRequestCommandOptions,
        items: string[],
        firstNameOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            // (8 bytes of the command) + (Length of the names selected by the user to multiple by the number of source names to send)
            this.options.lengthOfNames.byteLength * items.length + 8;

        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE.id)
            .writeUInt8(options.nameOfType) // Byte 1 : Name type
            .writeUInt8(options.lengthOfNames.type) // Byte 2 : Length of Names Required
            .writeUInt8(params.matrixId) // Byte 3 : Matrix Id
            .writeUInt8(params.levelId) // Byte 4 : Level Id
            .writeUInt8(Math.floor(firstNameOffsetId / 256)) // Byte 5 : First name number multiplier DIV 256
            .writeUInt8(firstNameOffsetId % 256) // Byte 6 : First name number MOD 256
            .writeUInt8(items.length); // Byte 7 : Number of names to follow ...
        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            buffer.writeString(items[itemIndex]);
        }
        return buffer.toBuffer();
    }

    /**
     * Validates the command parameters, options, items and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {UpdateNameRequestCommandParams} params the command parameters
     * @param {UpdateRenameRequestCommandOptions} options the command options
     * @param {UpdateRenameRequestCommandItems} items the command items
     * @memberof UpdateRenameRequestCommand
     */
    private validateParams(params: UpdateRenameRequestCommandParams, options: UpdateRenameRequestCommandOptions): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params, options).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
